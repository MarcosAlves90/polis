package spec

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"regexp"
	"strconv"
)

const (
	CoverageAdapterGoCoverProfileV1 = "go-coverprofile-v1"
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
	if len(total) == 0 {
		return CoverageMetric{}, errors.New("coverprofile contains zero executable lines")
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
