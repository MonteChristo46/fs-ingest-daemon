package ingest

import (
	"fs-ingest-daemon/internal/config"
	"fs-ingest-daemon/internal/store"
	"fs-ingest-daemon/internal/util"
	"log"
	"time"
)

type Ingester struct {
	cfg   *config.Config
	store *store.Store
	stop  chan struct{}
}

func NewIngester(cfg *config.Config, s *store.Store) *Ingester {
	return &Ingester{
		cfg:   cfg,
		store: s,
		stop:  make(chan struct{}),
	}
}

func (i *Ingester) Start() {
	go func() {
		// Poll loop
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				i.processBatch()
			case <-i.stop:
				return
			}
		}
	}()
}

func (i *Ingester) Stop() {
	close(i.stop)
}

func (i *Ingester) processBatch() {
	files, err := i.store.GetPendingFiles(10)
	if err != nil {
		log.Printf("Ingester: Error fetching pending files: %v", err)
		return
	}

	for _, f := range files {
		i.upload(f)
	}
}

func (i *Ingester) upload(f store.FileRecord) {
	// 1. Extract Metadata
	meta := util.ExtractMetadata(i.cfg.WatchPath, f.Path)
	
	// 2. Mock Upload
	log.Printf("[UPLOAD] Start: %s (Size: %d) Meta: %v -> Endpoint: %s", f.Path, f.Size, meta, i.cfg.Endpoint)
	
	// Simulate network delay
	time.Sleep(500 * time.Millisecond)

	// 3. Mark as Uploaded
	if err := i.store.MarkUploaded(f.Path); err != nil {
		log.Printf("Ingester: Failed to mark %s as uploaded: %v", f.Path, err)
	} else {
		log.Printf("[UPLOAD] Success: %s", f.Path)
	}
}
