package main

import (
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"skiff/internal/tasks"
)

// cmdTask lists the scheduled tasks or runs one now, so that the scheduler
// of the operating system (cron, Task Scheduler, launchd) can run them
// while Skiff is closed.
func cmdTask(args []string) int {
	fs := flag.NewFlagSet("task", flag.ContinueOnError)
	quiet := fs.Bool("q", false, "print only the result, not the log")
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: skiff task list\n       skiff task run [-q] <name or id>")
		fs.PrintDefaults()
	}
	if len(args) == 0 {
		fs.Usage()
		return 2
	}
	sub := args[0]
	if err := fs.Parse(args[1:]); err != nil {
		return 2
	}
	store, err := openStore()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	ts := tasks.OpenStore(store)
	switch sub {
	case "list", "ls":
		list := ts.Tasks()
		if len(list) == 0 {
			fmt.Println("No task. Create them in Skiff: palette → Scheduled tasks.")
			return 0
		}
		for _, t := range list {
			state := "on "
			if !t.Enabled {
				state = "off"
			}
			last := "never run"
			if r, ok := ts.LastRun(t.ID); ok {
				last = fmt.Sprintf("%s %s", r.Start.Format("2006-01-02 15:04"), r.Status)
			}
			fmt.Printf("%s  %-30s %-7s %-28s %s\n", state, t.Name, t.Kind, scheduleText(t.Schedule), last)
		}
		return 0
	case "run":
		if fs.NArg() != 1 {
			fs.Usage()
			return 2
		}
		t, ok := ts.Find(fs.Arg(0))
		if !ok {
			fmt.Fprintf(os.Stderr, "no task %q (skiff task list)\n", fs.Arg(0))
			return 1
		}
		ctx, cancel := signalContext()
		defer cancel()
		var onLine func(string)
		if !*quiet {
			onLine = func(s string) { fmt.Println(s) }
		}
		r := tasks.Execute(ctx, store, t, "command line", onLine)
		if err := ts.SaveRun(r); err != nil {
			fmt.Fprintln(os.Stderr, "history:", err)
		}
		fmt.Printf("%s: %s (%s)\n", strings.ToUpper(r.Status), r.Summary, r.End.Sub(r.Start).Round(time.Millisecond))
		if r.Status != "ok" {
			return 1
		}
		return 0
	}
	fs.Usage()
	return 2
}

func scheduleText(s tasks.Schedule) string {
	switch s.Kind {
	case "interval":
		return fmt.Sprintf("every %d min", s.Every)
	case "daily":
		return "daily at " + s.At
	case "weekly":
		names := []string{"Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"}
		var d []string
		for _, x := range s.Days {
			if x >= 0 && x < 7 {
				d = append(d, names[x])
			}
		}
		return strings.Join(d, ",") + " at " + s.At
	case "cron":
		return "cron " + s.Cron
	}
	return "manual"
}
