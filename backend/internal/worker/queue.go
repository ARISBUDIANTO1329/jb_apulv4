package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

type JobPayload struct {
	Type    string `json:"type"` // production/upload/livestream/shorts
	JobID   string `json:"job_id"`
	ChannelID string `json:"channel_id"`
	Payload json.RawMessage `json:"payload"`
}

type JobQueue struct {
	Redis *redis.Client
	ctx   context.Context
}

func NewJobQueue(redis *redis.Client) *JobQueue {
	return &JobQueue{
		Redis: redis,
		ctx:   context.Background(),
	}
}

func (q *JobQueue) Push(job JobPayload) error {
	data, err := json.Marshal(job)
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	return q.Redis.LPush(q.ctx, "job_queue", data).Err()
}

func (q *JobQueue) Pop(timeout time.Duration) (*JobPayload, error) {
	result, err := q.Redis.BRPop(q.ctx, timeout, "job_queue").Result()
	if err != nil {
		return nil, err
	}
	if len(result) < 2 {
		return nil, fmt.Errorf("invalid pop result")
	}
	var job JobPayload
	if err := json.Unmarshal([]byte(result[1]), &job); err != nil {
		return nil, fmt.Errorf("unmarshal: %w", err)
	}
	return &job, nil
}

func (q *JobQueue) PublishProgress(jobID, channelID string, progress int, status, message string) {
	data := map[string]interface{}{
		"job_id":     jobID,
		"channel_id": channelID,
		"progress":   progress,
		"status":     status,
		"message":    message,
		"timestamp":  time.Now().Unix(),
	}
	jsonData, _ := json.Marshal(data)
	// Publish to channel-specific and global progress channels
	q.Redis.Publish(q.ctx, fmt.Sprintf("progress:%s", jobID), jsonData)
	q.Redis.Publish(q.ctx, "progress:all", jsonData)
}