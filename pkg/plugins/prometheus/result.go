package prometheus

import (
	"encoding/json"
	"math"
	"strconv"
	"time"
	"unicode/utf8"
)

type envelope struct {
	Status    string `json:"status"`
	ErrorType string `json:"errorType"`
	Data      struct {
		ResultType string          `json:"resultType"`
		Result     json.RawMessage `json:"result"`
	} `json:"data"`
	Warnings []string `json:"warnings"`
	Infos    []string `json:"infos"`
}

func parseResult(body []byte, expected string, l limits, start, end time.Time) (Result, error) {
	var e envelope
	if !utf8.Valid(body) || json.Unmarshal(body, &e) != nil {
		return Result{}, failure("invalid_response")
	}
	if e.Status == "error" {
		switch e.ErrorType {
		case "timeout":
			return Result{}, &Error{Code: "timeout", Retryable: true}
		case "canceled":
			return Result{}, failure("query_canceled")
		case "bad_data", "execution":
			return Result{}, failure("query")
		default:
			return Result{}, failure("provider_failure")
		}
	}
	if e.Status != "success" || expected != "" && e.Data.ResultType != expected {
		return Result{}, failure("invalid_result_type")
	}
	result := Result{ResultType: e.Data.ResultType, Warnings: []Annotation{}, Infos: []Annotation{}}
	if len(e.Warnings) > 64 || len(e.Infos) > 64 {
		return Result{}, failure("invalid_response")
	}
	for _, warning := range e.Warnings {
		if warning == "" || len(warning) > 8192 {
			return Result{}, failure("invalid_response")
		}
		result.Warnings = append(result.Warnings, Annotation{Code: "provider_warning", Message: "Prometheus reported a warning; results may be partial."})
	}
	for _, info := range e.Infos {
		if info == "" || len(info) > 8192 {
			return Result{}, failure("invalid_response")
		}
		result.Infos = append(result.Infos, Annotation{Code: "provider_info", Message: "Prometheus reported a query annotation."})
	}
	switch e.Data.ResultType {
	case "matrix", "vector":
		var raw []struct {
			Metric     map[string]string `json:"metric"`
			Values     []json.RawMessage `json:"values"`
			Value      json.RawMessage   `json:"value"`
			Histograms json.RawMessage   `json:"histograms"`
			Histogram  json.RawMessage   `json:"histogram"`
		}
		if len(e.Data.Result) == 0 || string(e.Data.Result) == "null" || json.Unmarshal(e.Data.Result, &raw) != nil {
			return Result{}, failure("invalid_response")
		}
		if len(raw) > l.series {
			return Result{}, failure("response_limit")
		}
		result.Series = []Series{}
		count := 0
		seen := map[string]bool{}
		for _, row := range raw {
			if row.Metric == nil || len(row.Metric) > 64 {
				return Result{}, failure("invalid_response")
			}
			for name, value := range row.Metric {
				if name == "" || len(name) > 256 || len(value) > 4096 {
					return Result{}, failure("invalid_response")
				}
			}
			if len(row.Histogram) > 0 || len(row.Histograms) > 0 {
				return Result{}, failure("unsupported_sample")
			}
			identity, _ := json.Marshal(row.Metric)
			if seen[string(identity)] {
				return Result{}, failure("invalid_response")
			}
			seen[string(identity)] = true
			values := row.Values
			if e.Data.ResultType == "vector" {
				if len(row.Value) == 0 || row.Values != nil {
					return Result{}, failure("invalid_response")
				}
				values = []json.RawMessage{row.Value}
			} else if row.Values == nil || len(row.Value) > 0 {
				return Result{}, failure("invalid_response")
			}
			series := Series{Metric: row.Metric, Points: []Point{}}
			for _, value := range values {
				count++
				if count > l.samples {
					return Result{}, failure("response_limit")
				}
				point, err := numericPoint(value)
				if err != nil {
					return Result{}, err
				}
				if !start.IsZero() && (point.Timestamp.Before(start) || point.Timestamp.After(end)) {
					return Result{}, failure("invalid_sample")
				}
				if len(series.Points) > 0 && !point.Timestamp.After(series.Points[len(series.Points)-1].Timestamp) {
					return Result{}, failure("invalid_sample")
				}
				series.Points = append(series.Points, point)
			}
			result.Series = append(result.Series, series)
		}
	case "scalar":
		point, err := numericPoint(e.Data.Result)
		if err != nil {
			return Result{}, err
		}
		result.Scalar = &point
	case "string":
		at, value, err := samplePair(e.Data.Result)
		if err != nil || len(value) > 4096 {
			return Result{}, failure("invalid_sample")
		}
		result.Text = &StringPoint{Timestamp: at, Value: value}
	default:
		return Result{}, failure("invalid_result_type")
	}
	return result, nil
}
func numericPoint(raw json.RawMessage) (Point, error) {
	at, text, err := samplePair(raw)
	if err != nil {
		return Point{}, err
	}
	value, err := strconv.ParseFloat(text, 64)
	if err != nil || math.IsNaN(value) || math.IsInf(value, 0) {
		return Point{}, failure("invalid_sample")
	}
	return Point{Timestamp: at, Value: value}, nil
}
func samplePair(raw json.RawMessage) (time.Time, string, error) {
	var pair []json.RawMessage
	if json.Unmarshal(raw, &pair) != nil || len(pair) != 2 {
		return time.Time{}, "", failure("invalid_sample")
	}
	var timestamp json.Number
	var value string
	if len(pair[0]) == 0 || pair[0][0] == '"' || json.Unmarshal(pair[0], &timestamp) != nil || json.Unmarshal(pair[1], &value) != nil || string(pair[1]) == "null" {
		return time.Time{}, "", failure("invalid_sample")
	}
	seconds, err := timestamp.Float64()
	if err != nil || math.IsNaN(seconds) || math.IsInf(seconds, 0) || seconds < -62135596800 || seconds > 253402300799.999 {
		return time.Time{}, "", failure("invalid_sample")
	}
	// Retain PR #79's response precision instead of truncating to whole seconds.
	at := time.UnixMilli(int64(math.Round(seconds * 1000))).UTC()
	if at.Year() < 1 || at.Year() > 9999 {
		return time.Time{}, "", failure("invalid_sample")
	}
	return at, value, nil
}
