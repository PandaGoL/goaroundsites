package src

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
	"golang.org/x/sync/errgroup"
)

/*
Сегодня мы будем обходить заданные сайты и собирать в хэштаблицу результаты обхода: имя сайма, статус ответа, время ответа.
Если данные уже есть в хэштаблице - обновим их.
Ходить по сети мы будем раз в requestFrequency после предыдущего похода, печатать значения раз в секунду
(можете выбрать, другой промежуток).
Вам понадобятся мьютексы для безопасного доступа к хэштаблице.
Так же нужно корректно использоваться контекст, чтобы завершать работу функций при его отмене.
*/

const (
	// you can limit concurrent net request. It's optional
	MaxGoroutines = 3
	// timeout for net requests
	Timeout = 2 * time.Second
)

type SiteStatus struct {
	Name          string
	StatusCode    int
	TimeOfRequest time.Time
}

type Monitor struct {
	StatusMap        map[string]SiteStatus
	Mtx              *sync.Mutex
	G                errgroup.Group
	Sites            []string
	RequestFrequency time.Duration
}

func NewMonitor(sites []string, requestFrequency time.Duration) *Monitor {
	return &Monitor{
		StatusMap:        make(map[string]SiteStatus),
		Mtx:              &sync.Mutex{},
		Sites:            sites,
		RequestFrequency: requestFrequency,
	}
}

func (m *Monitor) Run(ctx context.Context) (err error) {
	// run printStatuses and checkSite in different goroutines
	m.G.SetLimit(MaxGoroutines)

	go func() {
		err = m.printStatuses(ctx)
	}()

	ticker := time.NewTicker(m.RequestFrequency)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			for _, v := range m.Sites {
				v := v
				m.G.Go(func() error {
					m.checkSite(ctx, v)
					return nil
				})
			}
		}
		if err = m.G.Wait(); err != nil && err != context.Canceled {
			break
		}
	}

	return err
}

func (m *Monitor) checkSite(ctx context.Context, site string) {
	// check site and write result to StatusMap

	client := http.Client{
		Timeout: Timeout,
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, site, nil)
	if err != nil {
		logrus.Errorf("Creqte request error: %s", err)
		return
	}

	resp, err := client.Do(req)
	if err != nil {
		logrus.Errorf("Cant do request error: %s", err)
		return
	}

	resp.Body.Close()

	m.Mtx.Lock()
	m.StatusMap[site] = SiteStatus{
		Name:          site,
		StatusCode:    resp.StatusCode,
		TimeOfRequest: time.Now(),
	}
	m.Mtx.Unlock()
}

func (m *Monitor) printStatuses(ctx context.Context) error {
	ticker := time.NewTicker(time.Second * 1)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			m.Mtx.Lock()
			for _, status := range m.StatusMap {
				fmt.Printf("%s, %d, %v\n", status.Name, status.StatusCode, status.TimeOfRequest)
			}
			fmt.Println()
			m.Mtx.Unlock()
		}
	}
}
