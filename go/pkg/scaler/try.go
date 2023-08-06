package scaler

import (
	"container/list"
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/AliyunContainerService/scaler/go/pkg/config"
	model "github.com/AliyunContainerService/scaler/go/pkg/model"
	platform_client "github.com/AliyunContainerService/scaler/go/pkg/platform_client"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

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
	qpsEntityList *list.List // 最近5min的qps，len=60
	// lastMinQPS               // 最近1min qps
	// qpsEntityMap   map[int64]*model.QpsEntity 1683859454
	directRemoveCnt int // 越大，资源分越高
	gcRemoveCnt     int // 越大，冷启动分高
	curIntanceCnt   int
	// resourceUsageScore int
	// resourceUsageScore int
	// reused  int // 越大，冷启动分高,节省 init time
	// created int // 越大，资源分高， 节省 resource cost
	// 理想状态, reused 少了，需要降低directRemoveCnt, created 少了，需要提高directRemoveCnt
	// 如果created < directRemoveCnt, 说明，reused 多，还可以再删一些
	// 如果created > directRemoveCnt, 说明，reused 少，需要少删一些
	wrongDecisionCnt    int   // 如果 need  destroy = true，且在 initTime / mem 的时间内，还是出现了created ，则属于bad case
	lastNeedDestoryTime int64 // 上一次
	lastQPS             int64
	lastTime            int64
}

func NewV2(metaData *model.Meta, config *config.Config) Scaler {
	client, err := platform_client.New(config.ClientAddr)
	if err != nil {
		log.Fatalf("client init with error: %s", err.Error())
	}
	scheduler := &Try{
		config:         config,
		metaData:       metaData,
		platformClient: client,
		mu:             sync.Mutex{},
		wg:             sync.WaitGroup{},
		instances:      make(map[string]*model.Instance),
		idleInstance:   list.New(),
		// qpsList:        make([]int64, 100000000),
		// startPoint:     1680278400,
		qpsEntityList:    list.New(),
		directRemoveCnt:  0,
		gcRemoveCnt:      0,
		curIntanceCnt:    0,
		wrongDecisionCnt: 0,
		lastQPS:          0,
		lastTime:         0,
		// lastMinQPS:       0,
	}
	log.Printf("New scaler for app: %s is created", metaData.Key)
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

	// 删除多余元素，位置1min的时间窗口
	if s.qpsEntityList.Len() > 10 {
		s.qpsEntityList.Remove(s.qpsEntityList.Front())
	}
	// 超出30min，也清除头
	front := s.qpsEntityList.Front().Value.(*model.QpsEntity)
	if front.CurrentTime < requestTime-10 {
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
	s.curIntanceCnt = s.curIntanceCnt + 1

	// 惩罚点, 在允许范围时间内，早删除了实例
	_, ok := config.Meta3Memory[request.MetaData.Key]
	_, ok2 := config.Meta3InitDurationMs[request.MetaData.Key]
	if ok && ok2 {
		if (requestTime - s.lastNeedDestoryTime) < 10*60*1000 {
			s.wrongDecisionCnt = s.wrongDecisionCnt + 1
		}
	}
	s.mu.Unlock()
	log.Printf("request id: %s, instance %s for app %s is created, init latency: %dms", request.RequestId, instance.Id, instance.Meta.Key, instance.InitDurationInMs)

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
	start := time.Now()
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
	data3Duration, ok := config.Meta3Duration[request.Assigment.MetaKey]
	// dd, _ := json.Marshal(data3Duration)
	data3Memory, ok2 := config.Meta3Memory[request.Assigment.MetaKey]
	data3InitDuration, ok3 := config.Meta3InitDurationMs[request.Assigment.MetaKey]
	// dm, _ := json.Marshal(data3Memory)
	// log.Printf("data3Duration: %v, data3Memory: %v, qpsEntityList: %v", data3Duration, data3Memory, s.qpsEntityList.Len())
	// var curQPS int
	var balancePodNums int
	requestTime := start.Unix()
	s.mu.Lock()
	curPodNums := len(s.instances)
	// stats := s.Stats()
	curPodNums2 := s.curIntanceCnt
	// curPodNums3 := s.Stats().TotalInstance
	curIdlePodNums := s.idleInstance.Len()
	var curTime int
	var score float64 = 0.0
	var a float64 = 0.0
	var b float64 = 1.0
	var c float64 = 0.0
	var d float64 = 0.0
	cnt := 0
	// 最近1分钟的数量
	lastMinQPS := 0
	for item := s.qpsEntityList.Front(); nil != item; item = item.Next() {
		cur := item.Value.(*model.QpsEntity)
		if cur.CurrentTime <= requestTime && cur.CurrentTime > requestTime-60 {
			cnt = cnt + cur.QPS
		}
	}

	lastMinQPS = cnt/s.qpsEntityList.Len() + 1
	// 启动时间 + 执行时间 + idle时间（近似20ms）
	durationPerPod := float64(data3Duration + 20)
	if durationPerPod > 1 {
		// 如果1s 中处理m个，那么 n qps 需要 n/m 个pod就够
		balancePodNums = int(float64(lastMinQPS) / (1000 / durationPerPod))
	} else {
		balancePodNums = int(float64(lastMinQPS) * (durationPerPod / 1000))
	}

	if ok && ok2 && ok3 && curIdlePodNums > 0 && curIdlePodNums >= balancePodNums {
		// 初始化时间+执行时间+调用时间
		// coldAllTime := (data3Duration + float64(data3InitDuration)) + 20
		// balancePodNums = int(float32(lastMinQPS)/float32(1000/coldAllTime)) + 1

		// wrongDesicionCost := 0
		// s1: 认为后面1s内，该pod不会被再利用
		if data3Duration != 0 {
			a = 0.5 * (float64(data3Memory) / float64(data3InitDuration))
		}
		if lastMinQPS != 0 {
			d = 0.5 * float64(curIdlePodNums) / float64(lastMinQPS)
		}
		if balancePodNums != 0 {
			c = 0.5 * float64(curIdlePodNums/balancePodNums)
		}
		// score = a + b + c + d
		// if score >= 1 {
		// 	needDestroy = true
		// }
		if a >= 0.5 {
			if d >= 0.5 {
				needDestroy = true
			} else {
				if c > 0.5 {
					needDestroy = true
				}
			}
		} else {
			if d > 0.25 {
				needDestroy = true
			} else {
				if c > 0.5 {
					needDestroy = true
				}
			}
		}
		total := s.directRemoveCnt + s.gcRemoveCnt
		if total != 0 {
			b = float64((total - s.wrongDecisionCnt) / total)
		}
		// 修正
		if b < 0.99 {
			needDestroy = false
		}
	}
	if request.Result != nil && request.Result.NeedDestroy != nil && *request.Result.NeedDestroy {
		needDestroy = true
	}
	if needDestroy {
		s.directRemoveCnt = s.directRemoveCnt + 1
		s.lastNeedDestoryTime = int64(curTime)
	} else {
		s.gcRemoveCnt = s.gcRemoveCnt + 1
	}
	log.Printf(`Idle, metaKey: %s, s.wrongDecisionCnt: %d, instance: %s, 
	requestTime: %d,  cur.time: %d, data3Duration: %f, data3InitDuration:%d, 
	data3Memory: %d, instance len: %d, instance len2: %d, lastMinQPS qps: %d, 
	balancePodNums: %d, s.idleInstance.Len(): %d,  needDestroy: %v, directRemoveCnt: %v, 
	gcRemoveCnt: %v, durationPerPod: %f,request.Result.NeedDestroy: %v`,
		request.Assigment.MetaKey, s.wrongDecisionCnt, request.Assigment.InstanceId,
		requestTime, curTime, data3Duration, data3InitDuration, data3Memory, curPodNums,
		curPodNums2, lastMinQPS, balancePodNums, curIdlePodNums, needDestroy, s.directRemoveCnt,
		s.gcRemoveCnt, durationPerPod, *request.Result.NeedDestroy)
	log.Printf("score: %f, a: %f, b: %f, c: %f, d: %f", score, a, b, c, d)
	s.mu.Unlock()
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

func (s *Try) deleteSlot(ctx context.Context, requestId, slotId, instanceId, metaKey, reason string) {
	// log.Printf("start delete Instance %s (Slot: %s) of app: %s", instanceId, slotId, metaKey)
	if err := s.platformClient.DestroySLot(ctx, requestId, slotId, reason); err != nil {
		log.Printf("delete Instance %s (Slot: %s) of app: %s failed with: %s", instanceId, slotId, metaKey, err.Error())
		s.mu.Lock()
		s.curIntanceCnt = s.curIntanceCnt - 1
		s.mu.Unlock()
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
