package clientupdate

import "sync"

type Progress struct {
	Stage      string `json:"stage"`
	Downloaded int64  `json:"downloaded"`
	Total      int64  `json:"total"`
}

var progressMu sync.RWMutex
var progress Progress

func GetProgress() Progress {
	progressMu.RLock()
	defer progressMu.RUnlock()
	return progress
}

func setStage(stage string) {
	progressMu.Lock()
	progress.Stage = stage
	progressMu.Unlock()
}

type downloadProgress struct{}

func (downloadProgress) Write(p []byte) (int, error) {
	progressMu.Lock()
	progress.Downloaded += int64(len(p))
	progressMu.Unlock()
	return len(p), nil
}
