package scaler

import (
	"container/list"
	"context"
	"fmt"
	"github.com/AliyunContainerService/scaler/go/pkg/config"
	model "github.com/AliyunContainerService/scaler/go/pkg/model"
	platform_client "github.com/AliyunContainerService/scaler/go/pkg/platform_client"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"log"
	"sync"
	"time"

	pb "github.com/AliyunContainerService/scaler/proto"
	"github.com/google/uuid"
)

type Try struct {
	config         *config.Config
	metaData       *model.Meta
	platformClient platform_client.Client
	mu             sync.Mutex
	wg             sync.WaitGroup
	instances      map[string]*model.Instance
	idleInstance   *list.List
	// qpsList        []int64    // qps list
	// startPoint     int64      // 20230601 1685548800 以来的 1683859454
	qpsEntityList *list.List // 最近5min的qps，len=300
	// qpsEntityMap   map[int64]*model.QpsEntity 1683859454
}

func NewV2(metaData *model.Meta, c *config.Config) Scaler {
	client, err := platform_client.New(c.ClientAddr)
	if err != nil {
		log.Fatalf("client init with error: %s", err.Error())
	}
	scheduler := &Try{
		config:         c,
		metaData:       metaData,
		platformClient: client,
		mu:             sync.Mutex{},
		wg:             sync.WaitGroup{},
		instances:      make(map[string]*model.Instance),
		idleInstance:   list.New(),
		// qpsList:        make([]int64, 100000000),
		// startPoint:     1680278400,
		qpsEntityList: list.New(),
	}
	log.Printf("New scaler for app: %s is created", metaData.Key)

	if contains(config.GlobalMetaKey1, metaData.Key) {
		*scheduler.config.IdleDurationBeforeGC = 5 * time.Minute
	} else if contains(config.GlobalMetaKey2, metaData.Key) {
		if metaData.MemoryInMb <= 256 {
			*scheduler.config.IdleDurationBeforeGC = 11 * time.Minute
		} else {
			*scheduler.config.IdleDurationBeforeGC = 3 * time.Minute
		}
	} else {
		*scheduler.config.IdleDurationBeforeGC = 3 * time.Minute
	}

	scheduler.wg.Add(1)
	go func() {
		defer scheduler.wg.Done()
		scheduler.gcLoop()
		log.Printf("gc loop for app: %s is stoped", metaData.Key)
	}()

	return scheduler
}
func (s *Try) Assign(ctx context.Context, request *pb.AssignRequest) (*pb.AssignReply, error) {
	start := time.Now()
	instanceId := uuid.New().String()

	// 记录qps，加锁
	requestTime := start.Unix()
	// jsonString, _ := json.Marshal(request)
	// log.Printf("Assign, request time: %s, request: %s", start, jsonString)
	s.mu.Lock()
	// qps 累加逻辑
	if s.qpsEntityList.Len() == 0 {
		tmp := &model.QpsEntity{
			CurrentTime: requestTime,
			QPS:         1,
		}
		s.qpsEntityList.PushBack(tmp)
	} else {
		cur := s.qpsEntityList.Back().Value.(*model.QpsEntity)
		if cur != nil {
			if cur.CurrentTime == requestTime {
				cur.QPS = cur.QPS + 1
			} else {
				tmp := &model.QpsEntity{
					CurrentTime: requestTime,
					QPS:         1,
				}
				s.qpsEntityList.PushBack(tmp)
			}
		} else {
			tmp := &model.QpsEntity{
				CurrentTime: requestTime,
				QPS:         1,
			}
			s.qpsEntityList.PushBack(tmp)
		}
	}
	// 删除多余元素，位置1h的时间窗口
	if s.qpsEntityList.Len() > 3600 {
		s.qpsEntityList.Remove(s.qpsEntityList.Front())
	}
	// 超出1h，也清除头
	front := s.qpsEntityList.Front().Value.(*model.QpsEntity)
	if front.CurrentTime < requestTime-3600 {
		s.qpsEntityList.Remove(s.qpsEntityList.Front())
	}

	s.mu.Unlock()

	defer func() {
		// log.Printf("Assign, request id: %s, instance id: %s, cost %dms", request.RequestId, instanceId, time.Since(start).Milliseconds())
	}()
	// log.Printf("Assign, request id: %s", request.RequestId)
	s.mu.Lock()
	if element := s.idleInstance.Front(); element != nil {
		instance := element.Value.(*model.Instance)
		instance.Busy = true
		s.idleInstance.Remove(element)
		s.mu.Unlock()
		log.Printf("Assign, metakey: %s, request id: %s, instance %s reused", request.MetaData.Key, request.RequestId, instance.Id)
		instanceId = instance.Id
		return &pb.AssignReply{
			Status: pb.Status_Ok,
			Assigment: &pb.Assignment{
				RequestId:  request.RequestId,
				MetaKey:    instance.Meta.Key,
				InstanceId: instance.Id,
			},
			ErrorMessage: nil,
		}, nil
	}
	s.mu.Unlock()

	//Create new Instance
	log.Printf("Assign, metakey: %s, request id: %s, instance %s create new", request.MetaData.Key, request.RequestId)
	resourceConfig := model.SlotResourceConfig{
		ResourceConfig: pb.ResourceConfig{
			MemoryInMegabytes: request.MetaData.MemoryInMb,
		},
	}
	slot, err := s.platformClient.CreateSlot(ctx, request.RequestId, &resourceConfig)
	if err != nil {
		errorMessage := fmt.Sprintf("create slot failed with: %s", err.Error())
		log.Printf(errorMessage)
		return nil, status.Errorf(codes.Internal, errorMessage)
	}

	meta := &model.Meta{
		Meta: pb.Meta{
			Key:           request.MetaData.Key,
			Runtime:       request.MetaData.Runtime,
			TimeoutInSecs: request.MetaData.TimeoutInSecs,
		},
	}
	instance, err := s.platformClient.Init(ctx, request.RequestId, instanceId, slot, meta)
	if err != nil {
		errorMessage := fmt.Sprintf("create instance failed with: %s", err.Error())
		log.Printf(errorMessage)
		return nil, status.Errorf(codes.Internal, errorMessage)
	}

	//add new instance
	s.mu.Lock()
	instance.Busy = true
	s.instances[instance.Id] = instance
	s.mu.Unlock()
	// log.Printf("request id: %s, instance %s for app %s is created, init latency: %dms", request.RequestId, instance.Id, instance.Meta.Key, instance.InitDurationInMs)

	return &pb.AssignReply{
		Status: pb.Status_Ok,
		Assigment: &pb.Assignment{
			RequestId:  request.RequestId,
			MetaKey:    instance.Meta.Key,
			InstanceId: instance.Id,
		},
		ErrorMessage: nil,
	}, nil
}

func contains(arr []string, target string) bool {
	for _, element := range arr {
		if element == target {
			return true
		}
	}
	return false
}

func (s *Try) Idle(ctx context.Context, request *pb.IdleRequest) (*pb.IdleReply, error) {
	if request.Assigment == nil {
		return nil, status.Errorf(codes.InvalidArgument, fmt.Sprintf("assignment is nil"))
	}
	reply := &pb.IdleReply{
		Status:       pb.Status_Ok,
		ErrorMessage: nil,
	}
	//start := time.Now()
	instanceId := request.Assigment.InstanceId
	defer func() {
		// log.Printf("Idle, request id: %s, instance: %s, cost %dus, data3Duration: %d", request.Assigment.RequestId, instanceId, time.Since(start).Microseconds(), len(config.Meta3Duration))
	}()
	// jsonStringIdle, _ := json.Marshal(request)
	// log.Printf("Idle, request: %v", jsonStringIdle)
	needDestroy := false
	slotId := ""

	// 针对数据集1 做优化
	// if contains(config.GlobalMetaKey1, request.Assigment.MetaKey) {
	// 	// 预测qps逻辑
	// 	// seer := &strategy.Seer{
	// 	// 	CurrentQPS: s.qpsList[start.Unix()-s.startPoint-60 : start.Unix()-s.startPoint],
	// 	// }
	// 	// jsonStringSeer, _ := json.Marshal(seer)
	// 	// log.Printf("Ilde, seer: %v", jsonStringSeer)
	// 	// increase := seer.PredictQPSIncrese(ctx)
	// 	// if !increase && s.idleInstance.Len() > 2 {
	// 	// 	needDestroy = true
	// 	// }
	// 	requestTime := start.Unix()
	// 	if s.qpsEntityList.Len() != 0 {
	// 		cur := s.qpsEntityList.Back().Value.(*model.QpsEntity)
	// 		if cur != nil {
	// 			if cur.CurrentTime <= requestTime {
	// 				balancePodNums := cur.QPS / int(1000/config.Meta1Duration[request.Assigment.MetaKey])
	// 				if len(s.instances) >= balancePodNums && s.idleInstance.Len() > 1 {
	// 					needDestroy = true
	// 				}
	// 			}
	// 		}
	// 	}
	// }
	// // 针对数据集2 做优化
	// if contains(config.GlobalMetaKey2, request.Assigment.MetaKey) {
	// 	requestTime := start.Unix()
	// 	if s.qpsEntityList.Len() != 0 {
	// 		cur := s.qpsEntityList.Back().Value.(*model.QpsEntity)
	// 		if cur != nil {
	// 			if cur.CurrentTime <= requestTime {
	// 				balancePodNums := cur.QPS / int(1000/config.Meta2Duration[request.Assigment.MetaKey])
	// 				if len(s.instances) >= balancePodNums && s.idleInstance.Len() > 1 {
	// 					needDestroy = true
	// 				}
	// 			}
	// 		}
	// 	}
	// }

	// 针对数据集3 做优化
	//data3Duration, ok1 := config.Meta3Duration[request.Assigment.MetaKey]
	data3MemoryMb, ok2 := config.Meta3Memory[request.Assigment.MetaKey]
	data3InitDurationMs, ok3 := config.Meta3InitDurationMs[request.Assigment.MetaKey]

	var alpha float32 = 2.5
	if ok2 && ok3 && s.qpsEntityList.Len() > 1 {
		// 按照多次取平均的方式计算QPS
		avgQPS := s.GetAvgQPS()
		/*
			选择1和选择2，哪个能在整体上带来更高的收益？假设两部分收益整体的比例关系为：alpha:1
			1. 如果此时销毁当前实例：在下一次使用当前实例时，重新初始化，代价为“调度总时间”，增加一次初始化时间
			2. 如果此时不销毁当前实例：则代价为“请求执行总消耗”，增加当前实例空闲时间*当前实例消耗的资源
			PS：精确的预测空闲时间比较好，这里先用平均QPS倒数估算一下
		*/
		contribute1 := float32(data3InitDurationMs) / float32(1000)
		contribute2 := float32(s.idleInstance.Len()+1) / avgQPS * float32(data3MemoryMb) / float32(1024)
		if alpha+1.0/(1.0+contribute1) > alpha/(1.0+contribute2)+1 {
			needDestroy = true
		}
	}

	if request.Result != nil && request.Result.NeedDestroy != nil && *request.Result.NeedDestroy {
		needDestroy = true
	}
	//log.Printf("Idle, metaKey: %s, data3Duration: %f, data3MemoryMB: %d, instance len: %d, cur qps: %d, balancePodNums: %d, s.idleInstance.Len(): %d,  needDestroy: %v",
	//	request.Assigment.MetaKey, data3Duration, data3MemoryMB, len(s.instances), curQPS, balancePodNums, s.idleInstance.Len(), needDestroy)
	defer func() {
		if needDestroy {
			s.deleteSlot(ctx, request.Assigment.RequestId, slotId, instanceId, request.Assigment.MetaKey, "bad instance")
		}
	}()
	// log.Printf("Idle, request id: %s", request.Assigment.RequestId)
	s.mu.Lock()
	defer s.mu.Unlock()
	if instance := s.instances[instanceId]; instance != nil {
		slotId = instance.Slot.Id
		instance.LastIdleTime = time.Now()
		if needDestroy {
			// log.Printf("request id %s, instance %s need be destroy", request.Assigment.RequestId, instanceId)
			return reply, nil
		}

		if instance.Busy == false {
			// log.Printf("request id %s, instance %s already freed", request.Assigment.RequestId, instanceId)
			return reply, nil
		}
		instance.Busy = false
		s.idleInstance.PushFront(instance)
	} else {
		return nil, status.Errorf(codes.NotFound, fmt.Sprintf("request id %s, instance %s not found", request.Assigment.RequestId, instanceId))
	}
	return &pb.IdleReply{
		Status:       pb.Status_Ok,
		ErrorMessage: nil,
	}, nil
}

func (s *Try) GetAvgQPS() float32 {
	if s.qpsEntityList.Len() <= 1 {
		return 0
	}
	cur1 := s.qpsEntityList.Back().Value.(*model.QpsEntity)
	cur2 := s.qpsEntityList.Back().Prev().Value.(*model.QpsEntity)
	avgQPSTotal := float32(cur2.QPS) / float32(cur1.CurrentTime-cur2.CurrentTime)
	total := 1
	if s.qpsEntityList.Len() > 2 {
		cur3 := s.qpsEntityList.Back().Prev().Prev().Value.(*model.QpsEntity)
		avgQPSTotal += float32(cur3.QPS) / float32(cur2.CurrentTime-cur3.CurrentTime)
		total++
		if s.qpsEntityList.Len() > 3 {
			cur4 := s.qpsEntityList.Back().Prev().Prev().Prev().Value.(*model.QpsEntity)
			avgQPSTotal += float32(cur4.QPS) / float32(cur3.CurrentTime-cur4.CurrentTime)
			total++
		}
	}
	avgQPS := avgQPSTotal / float32(total)
	return avgQPS
}

func (s *Try) deleteSlot(ctx context.Context, requestId, slotId, instanceId, metaKey, reason string) {
	// log.Printf("start delete Instance %s (Slot: %s) of app: %s", instanceId, slotId, metaKey)
	if err := s.platformClient.DestroySLot(ctx, requestId, slotId, reason); err != nil {
		// log.Printf("delete Instance %s (Slot: %s) of app: %s failed with: %s", instanceId, slotId, metaKey, err.Error())
	}
}

func (s *Try) gcLoop() {
	log.Printf("gc loop for app: %s is started", s.metaData.Key)
	ticker := time.NewTicker(s.config.GcInterval)
	for range ticker.C {
		for {
			s.mu.Lock()
			if element := s.idleInstance.Back(); element != nil {
				instance := element.Value.(*model.Instance)
				idleDuration := time.Now().Sub(instance.LastIdleTime)
				if idleDuration > *s.config.IdleDurationBeforeGC {
					//need GC
					s.idleInstance.Remove(element)
					delete(s.instances, instance.Id)
					s.mu.Unlock()
					go func() {
						reason := fmt.Sprintf("Idle duration: %fs, excceed configured duration: %fs", idleDuration.Seconds(), (*s.config.IdleDurationBeforeGC).Seconds())
						ctx := context.Background()
						ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
						defer cancel()
						s.deleteSlot(ctx, uuid.NewString(), instance.Slot.Id, instance.Id, instance.Meta.Key, reason)
					}()

					continue
				}
			}
			s.mu.Unlock()
			break
		}
	}
}

func (s *Try) Stats() Stats {
	s.mu.Lock()
	defer s.mu.Unlock()
	return Stats{
		TotalInstance:     len(s.instances),
		TotalIdleInstance: s.idleInstance.Len(),
	}
}
