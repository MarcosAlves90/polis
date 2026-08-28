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
	if !scanner.Scan() {
		if err := scanner.Err(); err != nil {
			return CoverageMetric{}, fmt.Errorf("read coverprofile: %w", err)
		}
		return CoverageMetric{}, errors.New("coverprofile is empty")
	}
	header := scanner.Text()
	if header != "mode: set" && header != "mode: count" && header != "mode: atomic" {
		return CoverageMetric{}, fmt.Errorf("unsupported coverprofile mode %q", header)
	}
	total := make(map[string]struct{})
	covered := make(map[string]struct{})
	lineNo := 1
	for scanner.Scan() {
		lineNo++
		line := scanner.Text()
		m := goCoverRecord.FindStringSubmatch(line)
		if m == nil {
			return CoverageMetric{}, fmt.Errorf("coverprofile line %d has invalid syntax", lineNo)
		}
		start, err := strconv.Atoi(m[2])
		if err != nil || start < 1 {
			return CoverageMetric{}, fmt.Errorf("coverprofile line %d has invalid start line", lineNo)
		}
		end, err := strconv.Atoi(m[4])
		if err != nil || end < start {
			return CoverageMetric{}, fmt.Errorf("coverprofile line %d has invalid end line", lineNo)
		}
		count, err := strconv.ParseUint(m[7], 10, 64)
		if err != nil {
			return CoverageMetric{}, fmt.Errorf("coverprofile line %d has invalid count", lineNo)
		}
		file := m[1]
		for sourceLine := start; sourceLine <= end; sourceLine++ {
			key := file + "\x00" + strconv.Itoa(sourceLine)
			total[key] = struct{}{}
			if count > 0 {
				covered[key] = struct{}{}
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return CoverageMetric{}, fmt.Errorf("read coverprofile: %w", err)
	}
	if len(total) == 0 {
		return CoverageMetric{}, errors.New("coverprofile contains zero executable lines")
	}
	percent := float64(len(covered)) / float64(len(total)) * 100
	return CoverageMetric{CoveredLines: len(covered), TotalLines: len(total), Percent: percent}, nil
}

func CoveragePass(valuePercent, thresholdPercent float64) bool {
	return valuePercent > thresholdPercent
}
