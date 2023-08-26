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

func setTotalInit(initDurationMs float32) {
	totalLock.Lock()
	defer totalLock.Unlock()
	totalInitTime += initDurationMs
	totalSaveTimes += 1
}

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
		*scheduler.config.IdleDurationBeforeGC = 16 * time.Second
	} else if contains(config.GlobalMetaKey2, metaData.Key) {
		*scheduler.config.IdleDurationBeforeGC = 200 * time.Second
	} else {
		*scheduler.config.IdleDurationBeforeGC = 12 * time.Second
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
	// 删除多余元素，位置30min的时间窗口
	if s.qpsEntityList.Len() > 1800 {
		s.qpsEntityList.Remove(s.qpsEntityList.Front())
	}
	// 超出30min，也清除头
	front := s.qpsEntityList.Front().Value.(*model.QpsEntity)
	if front.CurrentTime < requestTime-1800 {
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

	//// 设置全局初始化时间
	//if data3InitDurationMs, ok := config.Meta3InitDurationMs[request.MetaData.Key]; ok {
	//	totalLock.Lock()
	//	totalInitTime += float32(data3InitDurationMs) / 1000.0
	//	totalLock.Unlock()
	//}

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

	// 当前实例
	instance := s.instances[instanceId]
	if instance == nil {
		return nil, status.Errorf(codes.NotFound, fmt.Sprintf("request id %s, instance %s not found", request.Assigment.RequestId, instanceId))
	}

	memoryMb := instance.Slot.ResourceConfig.MemoryInMegabytes
	startDurationInMs := int64(instance.Slot.CreateDurationInMs) + instance.InitDurationInMs
	processDurationInMs := time.Now().UnixMilli() - instance.LastIdleTime.UnixMilli()

	//var avgSaveCost float32
	avgQPS := s.GetAvgQPS()
	if avgQPS > 0 {
		/*
			如果空闲实例数，超过了平均QPS，销毁也是情理之中（兜底）
		*/
		if float32(s.idleInstance.Len()) >= avgQPS {
			needDestroy = true
		}
		/*
			如果处理的耗时比较短，也不需要过多的实例
		*/
		idleTime := float32(s.idleInstance.Len()+1) * 1000.0 / avgQPS
		if !needDestroy && idleTime > float32(processDurationInMs) {
			needDestroy = true
		}
		/*
			如果选择直接销毁，可以节省很多代价，那么就选择销毁
		*/
		if !needDestroy && idleTime > float32(startDurationInMs) {
			saveCost := (idleTime - float32(startDurationInMs)) / 1000.0 * float32(memoryMb) / 1024.0
			if saveCost > 0.5 {
				needDestroy = true
			}
		}
	}

	/*
		新手保护，60秒内不删除
	*/
	if time.Now().UnixMilli()-instance.CreateTimeInMs <= 60000 {
		needDestroy = false
	}

	if request.Result != nil && request.Result.NeedDestroy != nil && *request.Result.NeedDestroy {
		needDestroy = true
	}

	log.Printf("【Idle】metaKey: %s, data3MemoryMb: %d, avgQPS: %f, startDurationInMs: %d, processDurationInMs:%d, s.idleInstance.Len(): %d, needDestroy: %v",
		request.Assigment.MetaKey, memoryMb, avgQPS, startDurationInMs, processDurationInMs, s.idleInstance.Len(), needDestroy)

	defer func() {
		if needDestroy {
			s.deleteSlot(ctx, request.Assigment.RequestId, slotId, instanceId, request.Assigment.MetaKey, "bad instance")
		}
	}()

	s.mu.Lock()
	defer s.mu.Unlock()

	slotId = instance.Slot.Id
	instance.LastIdleTime = time.Now()
	if needDestroy {
		return reply, nil
	}

	if instance.Busy == false {
		return reply, nil
	}
	instance.Busy = false
	s.idleInstance.PushFront(instance)

	return &pb.IdleReply{
		Status:       pb.Status_Ok,
		ErrorMessage: nil,
	}, nil
}

func (s *Try) GetAvgQPS() float32 {
	if s.qpsEntityList.Len() < 1 {
		return 0
	}

	var (
		avgQPSTotal float32 = 0
		total       float32 = 0

		now = time.Now().Unix()
	)

	cur1 := s.qpsEntityList.Back().Value.(*model.QpsEntity)
	avgQPSTotal += float32(cur1.QPS) / float32(now-cur1.CurrentTime+1)
	total++

	if s.qpsEntityList.Len() > 1 {
		cur2 := s.qpsEntityList.Back().Prev().Value.(*model.QpsEntity)
		avgQPSTotal += float32(cur2.QPS) / float32(cur1.CurrentTime-cur2.CurrentTime)
		total++

		if s.qpsEntityList.Len() > 2 {
			cur3 := s.qpsEntityList.Back().Prev().Prev().Value.(*model.QpsEntity)
			avgQPSTotal += float32(cur3.QPS) / float32(cur2.CurrentTime-cur3.CurrentTime)
			total++

			//if s.qpsEntityList.Len() > 3 {
			//	cur4 := s.qpsEntityList.Back().Prev().Prev().Prev().Value.(*model.QpsEntity)
			//	avgQPSTotal += float32(cur4.QPS) / float32(cur3.CurrentTime-cur4.CurrentTime)
			//	total++
			//}
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
