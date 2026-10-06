package cli

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"strings"
	"sync"
	"time"
)

type WorkItem struct {
	URL     string    `json:"url"`
	Status  string    `json:"status"` // pending|claimed|done|failed
	Claimed string    `json:"claimed"`
	DoneAt  time.Time `json:"done_at"`
	JobID   string    `json:"job_id"`
}

type WorkQueue struct {
	Items   map[string]*WorkItem `json:"items"`
	JobID   string               `json:"job_id"`
	Created time.Time            `json:"created"`
	mu      sync.Mutex
	path    string
}

func NewWorkQueue(path string, urls []string) (*WorkQueue, error) {
	wq := &WorkQueue{
		Items:   make(map[string]*WorkItem),
		JobID:   fmt.Sprintf("job-%d", time.Now().UnixNano()),
		Created: time.Now(),
		path:    path,
	}
	for _, u := range urls {
		ut := strings.TrimSpace(u)
		if ut == "" {
			continue
		}
		wq.Items[ut] = &WorkItem{URL: ut, Status: "pending"}
	}
	if err := wq.Save(); err != nil {
		return nil, err
	}
	return wq, nil
}

func LoadWorkQueue(path string) (*WorkQueue, error) {
	b, err := ioutil.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var wq WorkQueue
	if err := json.Unmarshal(b, &wq); err != nil {
		return nil, err
	}
	wq.path = path
	if wq.Items == nil {
		wq.Items = make(map[string]*WorkItem)
	}
	return &wq, nil
}

func (wq *WorkQueue) Save() error {
	wq.mu.Lock()
	defer wq.mu.Unlock()
	b, err := json.MarshalIndent(wq, "", "  ")
	if err != nil {
		return err
	}
	return ioutil.WriteFile(wq.path, b, 0644)
}

func (wq *WorkQueue) Claim(jobID string) (string, bool) {
	wq.mu.Lock()
	defer wq.mu.Unlock()
	for _, it := range wq.Items {
		if it.Status == "pending" {
			it.Status = "claimed"
			it.Claimed = jobID
			it.JobID = jobID
			wq.saveNoLock()
			return it.URL, true
		}
	}
	return "", false
}

func (wq *WorkQueue) Done(url, jobID string) {
	wq.mu.Lock()
	defer wq.mu.Unlock()
	if it, ok := wq.Items[url]; ok {
		it.Status = "done"
		it.JobID = jobID
		it.DoneAt = time.Now()
		wq.saveNoLock()
	}
}

func (wq *WorkQueue) Fail(url, jobID string) {
	wq.mu.Lock()
	defer wq.mu.Unlock()
	if it, ok := wq.Items[url]; ok && it.Status != "done" {
		it.Status = "failed"
		it.JobID = jobID
		wq.saveNoLock()
	}
}

func (wq *WorkQueue) saveNoLock() {
	b, _ := json.MarshalIndent(wq, "", "  ")
	ioutil.WriteFile(wq.path, b, 0644)
}

func readURLsList(path string) []string {
	var urls []string
	b, _ := ioutil.ReadFile(path)
	sc := bufio.NewScanner(strings.NewReader(string(b)))
	for sc.Scan() {
		lt := strings.TrimSpace(sc.Text())
		if lt == "" || strings.HasPrefix(lt, "#") {
			continue
		}
		urls = append(urls, lt)
	}
	return urls
}

func writeChunkWithFilter(src, dst string, keep func(string) bool) error {
	urls := readURLsList(src)
	var out []string
	for _, u := range urls {
		if keep(u) {
			out = append(out, u)
		}
	}
	f, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer f.Close()
	w := bufio.NewWriter(f)
	for _, u := range out {
		fmt.Fprintln(w, u)
	}
	w.Flush()
	return nil
}
