package scheduler

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/robfig/cron/v3"

	"magpiesync/internal/store"
)

const Key = "sync.schedule"

var parser = cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)

func Parse(expression string) (cron.Schedule, error) {
	expression = strings.TrimSpace(expression)
	if expression == "" {
		return nil, nil
	}
	if len(strings.Fields(expression)) != 5 {
		return nil, fmt.Errorf("请输入五段 Cron 表达式：分 时 日 月 周")
	}
	plan, err := parser.Parse(expression)
	if err != nil {
		return nil, fmt.Errorf("Cron 表达式无效：%w", err)
	}
	if plan.Next(time.Now()).IsZero() {
		return nil, fmt.Errorf("Cron 表达式没有可执行日期")
	}
	return plan, nil
}

func Load(ctx context.Context, st *store.Store, legacy string) (string, error) {
	value, found, err := st.GetSetting(ctx, Key)
	if err != nil {
		return "", err
	}
	if found {
		return value, nil
	}
	value = strings.TrimSpace(legacy)
	if _, err := Parse(value); err != nil {
		return "", err
	}
	if err := st.SetSettings(ctx, map[string]string{Key: value}, time.Now().Unix()); err != nil {
		return "", err
	}
	return value, nil
}

type Scheduler struct {
	mu    sync.Mutex
	cron  *cron.Cron
	entry cron.EntryID
	run   func()
}

func New(run func()) *Scheduler {
	s := &Scheduler{cron: cron.New(), run: run}
	s.cron.Start()
	return s
}

// Configure accepts a validated plan so applying a persisted change cannot fail.
func (s *Scheduler) Configure(plan cron.Schedule) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.entry != 0 {
		s.cron.Remove(s.entry)
		s.entry = 0
	}
	if plan != nil {
		s.entry = s.cron.Schedule(plan, cron.FuncJob(s.run))
	}
}

func (s *Scheduler) Stop() { <-s.cron.Stop().Done() }
