package strategy

import "context"

type Seer struct {
	CurrentTime int64
	CurrentQPS  int64
}

type God interface {
	predictQPSIncrese(ctx context.Context) bool
}

func (s *Seer) predictQPSIncrese(ctx context.Context) bool {
	// 1. 历史数据分析
	return true
}
