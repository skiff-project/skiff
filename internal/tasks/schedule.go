package tasks

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Schedule says when a task runs by itself.
type Schedule struct {
	// Kind: manual (never by itself), interval, daily, weekly or cron.
	Kind  string `json:"kind"`
	Every int    `json:"every,omitempty"` // interval: minutes between two runs
	At    string `json:"at,omitempty"`    // daily, weekly: "HH:MM", local time
	Days  []int  `json:"days,omitempty"`  // weekly: 0 = Sunday … 6 = Saturday
	Cron  string `json:"cron,omitempty"`  // "minute hour day-of-month month day-of-week"
}

// Validate reports a schedule that cannot work.
func (s Schedule) Validate() error {
	switch s.Kind {
	case "", "manual":
		return nil
	case "interval":
		if s.Every < 1 {
			return errors.New("the interval must be at least one minute")
		}
		return nil
	case "daily", "weekly":
		if _, _, err := clock(s.At); err != nil {
			return err
		}
		if s.Kind == "weekly" && len(s.Days) == 0 {
			return errors.New("choose at least one day of the week")
		}
		return nil
	case "cron":
		_, err := parseCron(s.Cron)
		return err
	}
	return fmt.Errorf("unknown schedule %q", s.Kind)
}

func clock(at string) (h, m int, err error) {
	hs, ms, ok := strings.Cut(strings.TrimSpace(at), ":")
	h, err1 := strconv.Atoi(hs)
	m, err2 := strconv.Atoi(ms)
	if !ok || err1 != nil || err2 != nil || h < 0 || h > 23 || m < 0 || m > 59 {
		return 0, 0, fmt.Errorf("invalid time %q (HH:MM)", at)
	}
	return h, m, nil
}

// Next returns the first time strictly after ref when the schedule runs
// (zero for a manual task).
func (s Schedule) Next(ref time.Time) time.Time {
	switch s.Kind {
	case "interval":
		if s.Every < 1 {
			return time.Time{}
		}
		return ref.Add(time.Duration(s.Every) * time.Minute)
	case "daily", "weekly":
		h, m, err := clock(s.At)
		if err != nil {
			return time.Time{}
		}
		days := "*"
		if s.Kind == "weekly" {
			var d []string
			for _, x := range s.Days {
				d = append(d, strconv.Itoa(x))
			}
			days = strings.Join(d, ",")
		}
		c, err := parseCron(fmt.Sprintf("%d %d * * %s", m, h, days))
		if err != nil {
			return time.Time{}
		}
		return c.next(ref)
	case "cron":
		c, err := parseCron(s.Cron)
		if err != nil {
			return time.Time{}
		}
		return c.next(ref)
	}
	return time.Time{}
}

// cron is a parsed five-field expression: the allowed values of each field.
type cron struct {
	min, hour, dom, month, dow [64]bool
	domAny, dowAny             bool
}

func parseField(f string, lo, hi int, set *[64]bool) (any bool, err error) {
	for _, part := range strings.Split(f, ",") {
		step := 1
		if base, st, ok := strings.Cut(part, "/"); ok {
			if step, err = strconv.Atoi(st); err != nil || step < 1 {
				return false, fmt.Errorf("invalid step in %q", part)
			}
			part = base
		}
		a, b := lo, hi
		switch {
		case part == "*":
			any = any || step == 1
		case strings.Contains(part, "-"):
			x, y, _ := strings.Cut(part, "-")
			if a, err = strconv.Atoi(x); err != nil {
				return false, fmt.Errorf("invalid value %q", part)
			}
			if b, err = strconv.Atoi(y); err != nil {
				return false, fmt.Errorf("invalid value %q", part)
			}
		default:
			if a, err = strconv.Atoi(part); err != nil {
				return false, fmt.Errorf("invalid value %q", part)
			}
			b = a
			if step > 1 { // "5/15": from 5 to the end
				b = hi
			}
		}
		if hi == 7 { // day of week: 7 is Sunday too
			if a == 7 {
				a, b = 0, 0
			} else if b == 7 {
				set[0] = true
				b = 6
			}
		}
		if a < lo || b > hi || a > b {
			return false, fmt.Errorf("value out of range in %q (%d-%d)", part, lo, hi)
		}
		for v := a; v <= b; v += step {
			set[v] = true
		}
	}
	return any, nil
}

func parseCron(expr string) (*cron, error) {
	f := strings.Fields(expr)
	if len(f) != 5 {
		return nil, errors.New("a cron expression has five fields: minute hour day month weekday")
	}
	c := &cron{}
	var err error
	if _, err = parseField(f[0], 0, 59, &c.min); err != nil {
		return nil, err
	}
	if _, err = parseField(f[1], 0, 23, &c.hour); err != nil {
		return nil, err
	}
	if c.domAny, err = parseField(f[2], 1, 31, &c.dom); err != nil {
		return nil, err
	}
	if _, err = parseField(f[3], 1, 12, &c.month); err != nil {
		return nil, err
	}
	if c.dowAny, err = parseField(f[4], 0, 7, &c.dow); err != nil {
		return nil, err
	}
	return c, nil
}

// dayMatches follows cron: when both day fields are restricted, either
// may match.
func (c *cron) dayMatches(t time.Time) bool {
	dom, dow := c.dom[t.Day()], c.dow[int(t.Weekday())]
	switch {
	case c.domAny && c.dowAny:
		return true
	case c.domAny:
		return dow
	case c.dowAny:
		return dom
	}
	return dom || dow
}

func (c *cron) next(ref time.Time) time.Time {
	t := ref.Truncate(time.Minute).Add(time.Minute)
	limit := t.AddDate(5, 0, 0)
	for t.Before(limit) {
		switch {
		case !c.month[int(t.Month())]:
			t = time.Date(t.Year(), t.Month()+1, 1, 0, 0, 0, 0, t.Location())
		case !c.dayMatches(t):
			t = time.Date(t.Year(), t.Month(), t.Day()+1, 0, 0, 0, 0, t.Location())
		case !c.hour[t.Hour()]:
			t = time.Date(t.Year(), t.Month(), t.Day(), t.Hour()+1, 0, 0, 0, t.Location())
		case !c.min[t.Minute()]:
			t = t.Add(time.Minute)
		default:
			return t
		}
	}
	return time.Time{}
}
