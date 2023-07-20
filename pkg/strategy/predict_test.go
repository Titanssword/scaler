package strategy

import (
	"container/list"
	"fmt"
	"testing"
	"time"

	"github.com/AliyunContainerService/scaler/pkg/model"
)

func TestArima(t *testing.T) {

	requestTime := time.Now().Unix()
	sqpsEntityList := list.New()
	for i := 1; i < 5; i++ {
		if sqpsEntityList.Len() == 0 {
			tmp := &model.QpsEntity{
				CurrentTime: requestTime,
				QPS:         1,
			}
			sqpsEntityList.PushBack(tmp)
		} else {
			cur := sqpsEntityList.Back().Value.(*model.QpsEntity)

			if cur != nil {
				if cur.CurrentTime == requestTime {
					cur.QPS = cur.QPS + 1
				} else {
					tmp := &model.QpsEntity{
						CurrentTime: requestTime,
						QPS:         1,
					}
					sqpsEntityList.PushBack(tmp)
				}
			} else {
				tmp := &model.QpsEntity{
					CurrentTime: requestTime,
					QPS:         1,
				}
				sqpsEntityList.PushBack(tmp)
			}
		}
	}

	// 遍历所有元素并打印其内容
	for e := sqpsEntityList.Front(); e != nil; e = e.Next() {
		fmt.Println(e.Value)
	}
}
