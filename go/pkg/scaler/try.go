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

var (
	totalLock              = sync.RWMutex{}
	totalInitTime  float32 = 0.0
	totalLock2             = sync.RWMutex{}
	totalSaveCost  float32 = 0.0
	totalSaveTimes float32 = 0.0
)

func getTotalSave() (float32, float32) {
	totalLock2.RLock()
	defer totalLock2.RUnlock()
	totalSaveCostTmp := totalSaveCost
	totalSaveTimesTmp := totalSaveTimes
	return totalSaveCostTmp, totalSaveTimesTmp
}

func setTotalSave(saveCost float32) {
	totalLock2.Lock()
	defer totalLock2.Unlock()
	totalSaveCost += saveCost
	totalSaveTimes += 1
}

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
		*scheduler.config.IdleDurationBeforeGC = 20 * time.Second
	} else if contains(config.GlobalMetaKey2, metaData.Key) {
		*scheduler.config.IdleDurationBeforeGC = 240 * time.Second
	} else {
		*scheduler.config.IdleDurationBeforeGC = 13 * time.Second
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
	// 删除多余元素，位置20min的时间窗口
	if s.qpsEntityList.Len() > 1200 {
		s.qpsEntityList.Remove(s.qpsEntityList.Front())
	}
	// 超出20min，也清除头
	front := s.qpsEntityList.Front().Value.(*model.QpsEntity)
	if front.CurrentTime < requestTime-1200 {
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
		//log.Printf("Assign, metakey: %s, request id: %s, instance %s reused", request.MetaData.Key, request.RequestId, instance.Id)
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
	//log.Printf("Assign, metakey: %s, request id: %s, instance %s create new", request.MetaData.Key, request.RequestId, instance.Id)

	//add new instance
	s.mu.Lock()
	instance.Busy = true
	s.instances[instance.Id] = instance
	s.mu.Unlock()
	// log.Printf("request id: %s, instance %s for app %s is created, init latency: %dms", request.RequestId, instance.Id, instance.Meta.Key, instance.InitDurationInMs)

	// 设置全局初始化时间
	if data3InitDurationMs, ok := config.Meta3InitDurationMs[request.MetaData.Key]; ok {
		totalLock.Lock()
		totalInitTime += float32(data3InitDurationMs) / 1000.0
		totalLock.Unlock()
	}

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

	//// 针对数据集3 做优化
	//_, ok1 := config.Meta3Duration[request.Assigment.MetaKey]
	//data3MemoryMb, ok2 := config.Meta3Memory[request.Assigment.MetaKey]
	//data3InitDurationMs, ok3 := config.Meta3InitDurationMs[request.Assigment.MetaKey]
	//
	//var avgSaveCost float32
	//avgQPS := s.GetAvgQPS()
	//if ok1 && ok2 && ok3 && avgQPS > 0 {
	//	/*
	//		获取初始化总耗时
	//	*/
	//	var totalInitTimeTmp float32
	//	totalLock.RLock()
	//	totalInitTimeTmp = totalInitTime
	//	totalLock.RUnlock()
	//	/*
	//		如果空闲实例数，超过了平均QPS，销毁也是情理之中（兜底）
	//	*/
	//	if float32(s.idleInstance.Len()) >= avgQPS {
	//		needDestroy = true
	//	}
	//	/*
	//		如果选择直接销毁，可以节省很多代价，那么就选择销毁
	//		前期只要有节省就销毁，中期必须超过节省代价的平均值120%才有销毁的意义，后期超过平均值150%就销毁
	//	*/
	//	idleTime := float32(s.idleInstance.Len()+1) * 1000.0 / avgQPS
	//	if idleTime > float32(data3InitDurationMs) {
	//		saveCost := (idleTime - float32(data3InitDurationMs)) / 1000.0 * float32(data3MemoryMb) / 1024.0
	//		if totalInitTimeTmp < 2000 {
	//			setTotalSave(saveCost)
	//			needDestroy = true
	//		} else {
	//			totalSaveCostTmp, totalSaveTimesTmp := getTotalSave()
	//			if totalSaveTimesTmp > 0 {
	//				avgSaveCost = totalSaveCostTmp / totalSaveTimesTmp
	//			}
	//			if totalInitTimeTmp < 6000 && saveCost > avgSaveCost*1.20 {
	//				setTotalSave(saveCost)
	//				needDestroy = true
	//			} else if saveCost > avgSaveCost*1.50 {
	//				setTotalSave(saveCost)
	//				needDestroy = true
	//			}
	//		}
	//		log.Printf("【Idle】metaKey: %s, idleLen: %d, avgQPS: %f, idleTime: %f, saveCost: %f, avgSaveCost: %f, totalInitTime: %f, needDestroy: %v",
	//			request.Assigment.MetaKey, s.idleInstance.Len(), avgQPS, idleTime, saveCost, avgSaveCost, totalInitTimeTmp, needDestroy)
	//	}
	//	/*
	//		如果初始化slot总耗时超过10000秒，则不希望再初始化，needDestroy = false
	//	*/
	//	if totalInitTimeTmp > 10000 {
	//		needDestroy = false
	//	}
	//}

	if request.Result != nil && request.Result.NeedDestroy != nil && *request.Result.NeedDestroy {
		needDestroy = true
	}

	//log.Printf("【Idle】metaKey: %s, data3MemoryMb: %d, avgQPS: %f, avgSaveCost: %f, s.idleInstance.Len(): %d, needDestroy: %v",
	//	request.Assigment.MetaKey, data3MemoryMb, avgQPS, avgSaveCost, s.idleInstance.Len(), needDestroy)

	defer func() {
		if needDestroy {
			s.deleteSlot(ctx, request.Assigment.RequestId, slotId, instanceId, request.Assigment.MetaKey, "bad instance")
		}
	}()

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
	if s.qpsEntityList.Len() < 1 {
		return 0
	}
	cur1 := s.qpsEntityList.Back().Value.(*model.QpsEntity)
	now := time.Now().Unix()
	if s.qpsEntityList.Len() == 1 && now == cur1.CurrentTime {
		return 0
	}

	var (
		avgQPSTotal float32 = 0
		total       float32 = 0
	)
	if now > cur1.CurrentTime {
		avgQPSTotal += float32(cur1.QPS) / float32(time.Now().Unix()-cur1.CurrentTime)
		total++
	}
	if s.qpsEntityList.Len() > 1 {
		cur2 := s.qpsEntityList.Back().Prev().Value.(*model.QpsEntity)
		avgQPSTotal += float32(cur2.QPS) / float32(cur1.CurrentTime-cur2.CurrentTime)
		total++
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
	}

	return avgQPSTotal / total
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
