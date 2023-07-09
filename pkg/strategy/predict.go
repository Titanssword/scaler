package strategy

import "context"

type Seer struct {
	// 当前请求时间
	CurrentTime int64
	// 最近一分钟的时间
	CurrentQPS []int64
}

type God interface {
	predictQPSIncrese(ctx context.Context) bool
}

func (s *Seer) predictQPSIncrese(ctx context.Context) bool {
	// 1. 历史数据分析
	return true
}
