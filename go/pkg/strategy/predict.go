package strategy

import "context"

type Seer struct {
	// 当前请求时间
	CurrentTime int64
	// 最近一分钟的时间
	CurrentQPS []int64
}

type God interface {
	PredictQPSIncrese(ctx context.Context) bool
}

func (s *Seer) PredictQPSIncrese(ctx context.Context) bool {
	// 计算整个数组的差值
	diff := s.CurrentQPS[len(s.CurrentQPS)-1] - s.CurrentQPS[0]
	midDiff := s.CurrentQPS[len(s.CurrentQPS)-1] - s.CurrentQPS[0+(len(s.CurrentQPS)-1)/2]
	if diff > 0 {
		return true
	} else {
		if midDiff > 0 {
			return true
		} else {
			return false
		}
	}
	// 1. 历史数据分析
	// return true
}
