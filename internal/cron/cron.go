package cron

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Schedule is a 5-field cron matcher (minute hour day-of-month month day-of-week).
type Schedule struct {
	minutes     map[int]struct{}
	hours       map[int]struct{}
	daysOfMonth map[int]struct{}
	months      map[int]struct{}
	daysOfWeek  map[int]struct{}
}

func Parse(expr string) (*Schedule, error) {
	expr = strings.TrimSpace(expr)
	if expr == "" {
		return nil, fmt.Errorf("cron expression is empty")
	}
	parts := strings.Fields(expr)
	if len(parts) != 5 {
		return nil, fmt.Errorf("cron expression must have 5 fields")
	}

	minutes, err := parseField(parts[0], 0, 59)
	if err != nil {
		return nil, fmt.Errorf("minute field: %w", err)
	}
	hours, err := parseField(parts[1], 0, 23)
	if err != nil {
		return nil, fmt.Errorf("hour field: %w", err)
	}
	dom, err := parseField(parts[2], 1, 31)
	if err != nil {
		return nil, fmt.Errorf("day-of-month field: %w", err)
	}
	months, err := parseField(parts[3], 1, 12)
	if err != nil {
		return nil, fmt.Errorf("month field: %w", err)
	}
	dow, err := parseField(parts[4], 0, 7)
	if err != nil {
		return nil, fmt.Errorf("day-of-week field: %w", err)
	}
	if _, ok := dow[7]; ok {
		dow[0] = struct{}{}
		delete(dow, 7)
	}

	return &Schedule{
		minutes:     minutes,
		hours:       hours,
		daysOfMonth: dom,
		months:      months,
		daysOfWeek:  dow,
	}, nil
}

func MustParse(expr string) *Schedule {
	s, err := Parse(expr)
	if err != nil {
		panic(err)
	}
	return s
}

func (s *Schedule) Matches(t time.Time) bool {
	_, ok := s.minutes[t.Minute()]
	if !ok {
		return false
	}
	_, ok = s.hours[t.Hour()]
	if !ok {
		return false
	}
	_, ok = s.daysOfMonth[t.Day()]
	if !ok {
		return false
	}
	_, ok = s.months[int(t.Month())]
	if !ok {
		return false
	}
	_, ok = s.daysOfWeek[int(t.Weekday())]
	return ok
}

func parseField(field string, min, max int) (map[int]struct{}, error) {
	result := make(map[int]struct{})
	parts := strings.Split(field, ",")
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			return nil, fmt.Errorf("empty segment")
		}

		switch {
		case p == "*":
			for i := min; i <= max; i++ {
				result[i] = struct{}{}
			}
		case strings.HasPrefix(p, "*/"):
			step, err := strconv.Atoi(strings.TrimPrefix(p, "*/"))
			if err != nil || step <= 0 {
				return nil, fmt.Errorf("invalid step %q", p)
			}
			for i := min; i <= max; i += step {
				result[i] = struct{}{}
			}
		case strings.Contains(p, "-"):
			rangeParts := strings.Split(p, "-")
			if len(rangeParts) != 2 {
				return nil, fmt.Errorf("invalid range %q", p)
			}
			start, err := strconv.Atoi(strings.TrimSpace(rangeParts[0]))
			if err != nil {
				return nil, fmt.Errorf("invalid range start in %q", p)
			}
			end, err := strconv.Atoi(strings.TrimSpace(rangeParts[1]))
			if err != nil {
				return nil, fmt.Errorf("invalid range end in %q", p)
			}
			if start > end || start < min || end > max {
				return nil, fmt.Errorf("range out of bounds %q", p)
			}
			for i := start; i <= end; i++ {
				result[i] = struct{}{}
			}
		default:
			v, err := strconv.Atoi(p)
			if err != nil {
				return nil, fmt.Errorf("invalid value %q", p)
			}
			if v < min || v > max {
				return nil, fmt.Errorf("value %d out of range [%d-%d]", v, min, max)
			}
			result[v] = struct{}{}
		}
	}

	if len(result) == 0 {
		return nil, fmt.Errorf("empty field")
	}
	return result, nil
}
