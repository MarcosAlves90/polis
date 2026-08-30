package spec

import (
	"bufio"
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"
)

const (
	CoverageAdapterGoCoverProfileV1 = "go-coverprofile-v1"
	CoverageAdapterLCOVV1           = "lcov-v1"
	CoverageAdapterCoberturaV1      = "cobertura-v1"
	CoverageMetricLinePercent       = "line_coverage_percent"
	CoverageOperatorGreaterThan     = ">"
	MinimumCoverageThreshold        = 80.0
)

type CoverageMetric struct {
	CoveredLines int
	TotalLines   int
	Percent      float64
}

var goCoverRecord = regexp.MustCompile(`^(.+):([0-9]+)\.([0-9]+),([0-9]+)\.([0-9]+) ([0-9]+) ([0-9]+)$`)

func ParseCoverage(adapter string, raw []byte) (CoverageMetric, error) {
	switch adapter {
	case CoverageAdapterGoCoverProfileV1:
		return ParseGoCoverProfile(raw)
	case CoverageAdapterLCOVV1:
		return ParseLCOV(raw)
	case CoverageAdapterCoberturaV1:
		return ParseCobertura(raw)
	default:
		return CoverageMetric{}, fmt.Errorf("unsupported coverage adapter %q", adapter)
	}
}

func ParseGoCoverProfile(raw []byte) (CoverageMetric, error) {
	scanner := bufio.NewScanner(bytes.NewReader(raw))
	scanner.Buffer(make([]byte, 64*1024), 16*1024*1024)
	if err := validateCoverProfileHeader(scanner); err != nil {
		return CoverageMetric{}, err
	}
	total := make(map[string]struct{})
	covered := make(map[string]struct{})
	if err := scanCoverProfileRecords(scanner, total, covered); err != nil {
		return CoverageMetric{}, err
	}
	return coverageMetricFromSets(total, covered, "coverprofile")
}

func ParseLCOV(raw []byte) (CoverageMetric, error) {
	scanner := bufio.NewScanner(bytes.NewReader(raw))
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	state := newLCOVState()
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		if err := state.consume(strings.TrimSpace(scanner.Text()), lineNo); err != nil {
			return CoverageMetric{}, err
		}
	}
	if err := scanner.Err(); err != nil {
		return CoverageMetric{}, fmt.Errorf("read lcov: %w", err)
	}
	return coverageMetricFromSets(state.total, state.covered, "lcov")
}

type lcovState struct {
	currentFile string
	total       map[string]struct{}
	covered     map[string]struct{}
}

func newLCOVState() *lcovState {
	return &lcovState{total: map[string]struct{}{}, covered: map[string]struct{}{}}
}

func (s *lcovState) consume(line string, lineNo int) error {
	switch {
	case strings.HasPrefix(line, "SF:"):
		return s.setSource(line, lineNo)
	case strings.HasPrefix(line, "DA:"):
		return s.addData(line, lineNo)
	case line == "end_of_record":
		s.currentFile = ""
	}
	return nil
}

func (s *lcovState) setSource(line string, lineNo int) error {
	s.currentFile = strings.TrimSpace(strings.TrimPrefix(line, "SF:"))
	if s.currentFile == "" {
		return fmt.Errorf("lcov line %d has empty source file", lineNo)
	}
	return nil
}

func (s *lcovState) addData(line string, lineNo int) error {
	if s.currentFile == "" {
		return fmt.Errorf("lcov line %d has DA before SF", lineNo)
	}
	parts := strings.Split(strings.TrimPrefix(line, "DA:"), ",")
	if len(parts) < 2 {
		return fmt.Errorf("lcov line %d has invalid DA record", lineNo)
	}
	sourceLine, err := strconv.Atoi(parts[0])
	if err != nil || sourceLine < 1 {
		return fmt.Errorf("lcov line %d has invalid source line", lineNo)
	}
	hits, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil || hits < 0 {
		return fmt.Errorf("lcov line %d has invalid hit count", lineNo)
	}
	s.record(sourceLine, hits)
	return nil
}

func (s *lcovState) record(sourceLine int, hits int64) {
	key := s.currentFile + "\x00" + strconv.Itoa(sourceLine)
	s.total[key] = struct{}{}
	if hits > 0 {
		s.covered[key] = struct{}{}
	}
}

func ParseCobertura(raw []byte) (CoverageMetric, error) {
	decoder := xml.NewDecoder(bytes.NewReader(raw))
	state := newCoberturaState()
	for {
		tok, err := decoder.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return CoverageMetric{}, fmt.Errorf("decode cobertura: %w", err)
		}
		if err := state.consume(tok); err != nil {
			return CoverageMetric{}, err
		}
	}
	return coverageMetricFromSets(state.total, state.covered, "cobertura")
}

type coberturaState struct {
	currentFile string
	classDepth  int
	total       map[string]struct{}
	covered     map[string]struct{}
}

func newCoberturaState() *coberturaState {
	return &coberturaState{total: map[string]struct{}{}, covered: map[string]struct{}{}}
}

func (s *coberturaState) consume(tok xml.Token) error {
	switch t := tok.(type) {
	case xml.StartElement:
		return s.startElement(t)
	case xml.EndElement:
		s.endElement(t)
	}
	return nil
}

func (s *coberturaState) startElement(element xml.StartElement) error {
	switch element.Name.Local {
	case "class":
		s.classDepth++
		s.currentFile = xmlAttr(element, "filename")
	case "line":
		return s.addLine(element)
	}
	return nil
}

func (s *coberturaState) addLine(element xml.StartElement) error {
	if s.classDepth == 0 || s.currentFile == "" {
		return nil
	}
	number, err := strconv.Atoi(xmlAttr(element, "number"))
	if err != nil || number < 1 {
		return errors.New("cobertura contains invalid line number")
	}
	hits, err := strconv.ParseInt(xmlAttr(element, "hits"), 10, 64)
	if err != nil || hits < 0 {
		return errors.New("cobertura contains invalid line hits")
	}
	s.recordLine(number, hits)
	return nil
}

func (s *coberturaState) recordLine(number int, hits int64) {
	key := s.currentFile + "\x00" + strconv.Itoa(number)
	s.total[key] = struct{}{}
	if hits > 0 {
		s.covered[key] = struct{}{}
	}
}

func (s *coberturaState) endElement(element xml.EndElement) {
	if element.Name.Local != "class" || s.classDepth == 0 {
		return
	}
	s.classDepth--
	if s.classDepth == 0 {
		s.currentFile = ""
	}
}

func xmlAttr(element xml.StartElement, name string) string {
	for _, attr := range element.Attr {
		if attr.Name.Local == name {
			return attr.Value
		}
	}
	return ""
}

func coverageMetricFromSets(total, covered map[string]struct{}, label string) (CoverageMetric, error) {
	if len(total) == 0 {
		return CoverageMetric{}, fmt.Errorf("%s contains zero executable lines", label)
	}
	percent := float64(len(covered)) / float64(len(total)) * 100
	return CoverageMetric{CoveredLines: len(covered), TotalLines: len(total), Percent: percent}, nil
}

func validateCoverProfileHeader(scanner *bufio.Scanner) error {
	if !scanner.Scan() {
		if err := scanner.Err(); err != nil {
			return fmt.Errorf("read coverprofile: %w", err)
		}
		return errors.New("coverprofile is empty")
	}
	header := scanner.Text()
	if header != "mode: set" && header != "mode: count" && header != "mode: atomic" {
		return fmt.Errorf("unsupported coverprofile mode %q", header)
	}
	return nil
}

func scanCoverProfileRecords(scanner *bufio.Scanner, total, covered map[string]struct{}) error {
	lineNo := 1
	for scanner.Scan() {
		lineNo++
		record, err := parseCoverRecord(scanner.Text(), lineNo)
		if err != nil {
			return err
		}
		addCoverRecord(record, total, covered)
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("read coverprofile: %w", err)
	}
	return nil
}

type coverRecord struct {
	file        string
	start, end  int
	coveredLine bool
}

func parseCoverRecord(line string, lineNo int) (coverRecord, error) {
	m := goCoverRecord.FindStringSubmatch(line)
	if m == nil {
		return coverRecord{}, fmt.Errorf("coverprofile line %d has invalid syntax", lineNo)
	}
	start, err := strconv.Atoi(m[2])
	if err != nil || start < 1 {
		return coverRecord{}, fmt.Errorf("coverprofile line %d has invalid start line", lineNo)
	}
	end, err := strconv.Atoi(m[4])
	if err != nil || end < start {
		return coverRecord{}, fmt.Errorf("coverprofile line %d has invalid end line", lineNo)
	}
	count, err := strconv.ParseUint(m[7], 10, 64)
	if err != nil {
		return coverRecord{}, fmt.Errorf("coverprofile line %d has invalid count", lineNo)
	}
	return coverRecord{file: m[1], start: start, end: end, coveredLine: count > 0}, nil
}

func addCoverRecord(record coverRecord, total, covered map[string]struct{}) {
	for sourceLine := record.start; sourceLine <= record.end; sourceLine++ {
		key := record.file + "\x00" + strconv.Itoa(sourceLine)
		total[key] = struct{}{}
		if record.coveredLine {
			covered[key] = struct{}{}
		}
	}
}

func CoveragePass(valuePercent, thresholdPercent float64) bool {
	return valuePercent > thresholdPercent
}
