package cronexpr

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

const maxSearchMinutes = 5 * 366 * 24 * 60

var koreaLocation = func() *time.Location {
	location, err := time.LoadLocation("Asia/Seoul")
	if err != nil {
		return time.FixedZone("KST", 9*60*60)
	}
	return location
}()

type Schedule struct {
	minute     fieldSet
	hour       fieldSet
	dayOfMonth fieldSet
	month      fieldSet
	dayOfWeek  fieldSet
}

type fieldSet struct {
	wildcard bool
	values   map[int]bool
}

func KST() *time.Location {
	return koreaLocation
}

func Parse(expression string) (Schedule, error) {
	fields := strings.Fields(expression)
	if len(fields) != 5 {
		return Schedule{}, fmt.Errorf("cron expression must have 5 fields")
	}
	minute, err := parseField(fields[0], 0, 59, false)
	if err != nil {
		return Schedule{}, fmt.Errorf("minute field: %w", err)
	}
	hour, err := parseField(fields[1], 0, 23, false)
	if err != nil {
		return Schedule{}, fmt.Errorf("hour field: %w", err)
	}
	dayOfMonth, err := parseField(fields[2], 1, 31, false)
	if err != nil {
		return Schedule{}, fmt.Errorf("day-of-month field: %w", err)
	}
	month, err := parseField(fields[3], 1, 12, false)
	if err != nil {
		return Schedule{}, fmt.Errorf("month field: %w", err)
	}
	dayOfWeek, err := parseField(fields[4], 0, 7, true)
	if err != nil {
		return Schedule{}, fmt.Errorf("day-of-week field: %w", err)
	}
	return Schedule{
		minute:     minute,
		hour:       hour,
		dayOfMonth: dayOfMonth,
		month:      month,
		dayOfWeek:  dayOfWeek,
	}, nil
}

func (s Schedule) NextAfter(after time.Time) (time.Time, bool) {
	candidate := after.In(KST()).Truncate(time.Minute).Add(time.Minute)
	for range maxSearchMinutes {
		if s.matches(candidate) {
			return candidate, true
		}
		candidate = candidate.Add(time.Minute)
	}
	return time.Time{}, false
}

func (s Schedule) matches(value time.Time) bool {
	if !s.minute.matches(value.Minute()) || !s.hour.matches(value.Hour()) || !s.month.matches(int(value.Month())) {
		return false
	}
	dayOfMonthMatches := s.dayOfMonth.matches(value.Day())
	dayOfWeekMatches := s.dayOfWeek.matches(int(value.Weekday()))
	if !s.dayOfMonth.wildcard && !s.dayOfWeek.wildcard {
		return dayOfMonthMatches || dayOfWeekMatches
	}
	return dayOfMonthMatches && dayOfWeekMatches
}

func parseField(raw string, minValue int, maxValue int, normalizeSunday bool) (fieldSet, error) {
	if raw == "" {
		return fieldSet{}, fmt.Errorf("empty field")
	}
	values := map[int]bool{}
	wildcard := true
	for _, part := range strings.Split(raw, ",") {
		if part == "" {
			return fieldSet{}, fmt.Errorf("empty list item")
		}
		partValues, partWildcard, err := parseFieldPart(part, minValue, maxValue, normalizeSunday)
		if err != nil {
			return fieldSet{}, err
		}
		wildcard = wildcard && partWildcard
		for _, value := range partValues {
			values[value] = true
		}
	}
	return fieldSet{wildcard: wildcard, values: values}, nil
}

func parseFieldPart(raw string, minValue int, maxValue int, normalizeSunday bool) ([]int, bool, error) {
	base, step, err := splitStep(raw)
	if err != nil {
		return nil, false, err
	}
	start, end, wildcard, err := parseRange(base, minValue, maxValue)
	if err != nil {
		return nil, false, err
	}
	values := make([]int, 0, end-start+1)
	for value := start; value <= end; value += step {
		if normalizeSunday && value == 7 {
			values = append(values, 0)
			continue
		}
		values = append(values, value)
	}
	return values, wildcard, nil
}

func splitStep(raw string) (string, int, error) {
	parts := strings.Split(raw, "/")
	if len(parts) > 2 {
		return "", 0, fmt.Errorf("invalid step")
	}
	if len(parts) == 1 {
		return raw, 1, nil
	}
	step, err := strconv.Atoi(parts[1])
	if err != nil || step <= 0 {
		return "", 0, fmt.Errorf("invalid step")
	}
	return parts[0], step, nil
}

func parseRange(raw string, minValue int, maxValue int) (int, int, bool, error) {
	if raw == "*" {
		return minValue, maxValue, true, nil
	}
	parts := strings.Split(raw, "-")
	if len(parts) > 2 {
		return 0, 0, false, fmt.Errorf("invalid range")
	}
	start, err := parseNumber(parts[0], minValue, maxValue)
	if err != nil {
		return 0, 0, false, err
	}
	if len(parts) == 1 {
		return start, start, false, nil
	}
	end, err := parseNumber(parts[1], minValue, maxValue)
	if err != nil {
		return 0, 0, false, err
	}
	if start > end {
		return 0, 0, false, fmt.Errorf("range start is after end")
	}
	return start, end, false, nil
}

func parseNumber(raw string, minValue int, maxValue int) (int, error) {
	value, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("invalid number")
	}
	if value < minValue || value > maxValue {
		return 0, fmt.Errorf("value out of range")
	}
	return value, nil
}

func (f fieldSet) matches(value int) bool {
	return f.values[value]
}
