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
	G2               errgroup.Group
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

func (m *Monitor) Run(ctx context.Context) error {
	// run printStatuses and checkSite in different goroutines
	siteChan := make(chan string)
	m.G.Go(func() error {
		return m.printStatuses(ctx)
	})

	go func() {
		for _, site := range m.Sites {
			siteChan <- site
		}
		close(siteChan)
	}()

	for i := 0; i < MaxGoroutines; i++ {
		m.G.Go(func() error {
			ticker := time.NewTicker(m.RequestFrequency)

			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-ticker.C:
					v, ok := <-siteChan
					if ok {
						m.checkSite(ctx, v)
					}
				}
			}
		})
	}

	if err := m.G.Wait(); err != nil && err != context.Canceled {
		return err
	}
	return nil
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
				fmt.Printf("%s, %d, %v", status.Name, status.StatusCode, status.TimeOfRequest)
			}
			fmt.Println()
			m.Mtx.Unlock()
		}
	}
}
