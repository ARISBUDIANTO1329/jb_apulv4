package worker

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jbapul/jb_apulv4/internal/config"
	"github.com/redis/go-redis/v9"
)

type Manager struct {
	DB         *pgxpool.Pool
	Cfg        *config.Config
	RDB        *redis.Client
	queue      *JobQueue
	ctx        context.Context
	cancel     context.CancelFunc
	wg         sync.WaitGroup
	production *ProductionWorker
	upload     *UploadWorker
	shorts     *ShortsWorker
	livestream *LivestreamWorker
}

func NewManager(db *pgxpool.Pool, cfg *config.Config, rdb *redis.Client) *Manager {
	ctx, cancel := context.WithCancel(context.Background())
	queue := NewJobQueue(rdb)
	return &Manager{
		DB:     db,
		Cfg:    cfg,
		RDB:    rdb,
		queue:  queue,
		ctx:    ctx,
		cancel: cancel,
		production: NewProductionWorker(db, cfg, queue),
		upload:     NewUploadWorker(db, cfg, queue),
		shorts:     NewShortsWorker(db, cfg, queue),
		livestream: NewLivestreamWorker(db, cfg, queue),
	}
}

func (m *Manager) Start() {
	log.Println("[Worker] Starting all workers with Redis queue...")

	m.wg.Add(4)
	go m.runWorker("production", func(ctx context.Context) { m.production.Run(ctx) })
	go m.runWorker("upload", func(ctx context.Context) { m.upload.Run(ctx) })
	go m.runWorker("shorts", func(ctx context.Context) { m.shorts.Run(ctx) })
	go m.runWorker("livestream", func(ctx context.Context) { m.livestream.Run(ctx) })
}

func (m *Manager) Stop() {
	log.Println("[Worker] Stopping all workers...")
	m.cancel()
	m.wg.Wait()
	log.Println("[Worker] All workers stopped")
}

func (m *Manager) runWorker(name string, run func(context.Context)) {
	defer m.wg.Done()
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[Worker:%s] Panic recovered: %v, restarting...", name, r)
			time.Sleep(5 * time.Second)
			go m.runWorker(name, run)
		}
	}()

	log.Printf("[Worker:%s] Started", name)
	run(m.ctx)
	log.Printf("[Worker:%s] Stopped", name)
}