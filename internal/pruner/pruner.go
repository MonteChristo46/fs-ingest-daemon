package pruner

import (
	"fmt"
	"fs-ingest-daemon/internal/config"
	"fs-ingest-daemon/internal/store"
	"log"
	"os"
	"time"
)

type Pruner struct {
	cfg   *config.Config
	store *store.Store
	stop  chan struct{}
}

func NewPruner(cfg *config.Config, s *store.Store) *Pruner {
	return &Pruner{
		cfg:   cfg,
		store: s,
		stop:  make(chan struct{}),
	}
}

func (p *Pruner) Start() {
	ticker := time.NewTicker(1 * time.Minute)
	go func() {
		for {
			select {
			case <-ticker.C:
				p.Prune()
			case <-p.stop:
				ticker.Stop()
				return
			}
		}
	}()
}

func (p *Pruner) Stop() {
	close(p.stop)
}

func (p *Pruner) Prune() {
	maxBytes := int64(p.cfg.MaxDataSizeGB * 1024 * 1024 * 1024)
	
	currentSize, err := p.store.GetTotalSize()
	if err != nil {
		log.Printf("Pruner: Error getting total size: %v", err)
		return
	}

	if currentSize <= maxBytes {
		return // Nothing to do
	}

	fmt.Printf("Pruner: Current size %d bytes > Max %d bytes. Starting eviction.\n", currentSize, maxBytes)

	// Fetch candidates
	candidates, err := p.store.GetPruneCandidates(50)
	if err != nil {
		log.Printf("Pruner: Error fetching candidates: %v", err)
		return
	}

	if len(candidates) == 0 {
		fmt.Println("Pruner: Disk full but no UPLOADED files to delete! Backpressure active.")
		return
	}

	for _, f := range candidates {
		// Verify size again? No, just delete until we are under
		// Actually, we should check size after each delete, or just delete the batch.
		// Let's delete the batch.
		
		err := os.Remove(f.Path)
		if err != nil && !os.IsNotExist(err) {
			log.Printf("Pruner: Failed to remove file %s: %v", f.Path, err)
			continue
		}

		if err := p.store.RemoveFile(f.Path); err != nil {
			log.Printf("Pruner: Failed to remove DB record for %s: %v", f.Path, err)
		} else {
			fmt.Printf("Pruned: %s\n", f.Path)
		}

		// Optimization: Check if we are under limit now?
		// For simplicity, we just process the batch. Next tick will check again.
	}
}
