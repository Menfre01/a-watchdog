package config

import (
	"context"
	"sync"
	"testing"
)

func TestSnapshot_RefreshIsAtomicAndConsistent(t *testing.T) {
	s := newTestStore(t)

	before := s.Snapshot()
	p := mustProvider(t, s, "deepseek")
	after := s.Snapshot()

	if before == after {
		t.Fatalf("snapshot pointer unchanged after write, want atomic swap")
	}
	// 旧视图保持不变:已持有的引用不随后续写入变化(单次请求内视图一致)。
	if _, ok := before.LookupProvider(p.ID); ok {
		t.Fatalf("旧快照包含新写入的 provider")
	}
	if info, ok := after.LookupProvider(p.ID); !ok || info.Name != "deepseek" {
		t.Fatalf("新快照缺少 provider: %+v, %v", info, ok)
	}
}

func TestSnapshot_ConcurrentReadWrite(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	var wg sync.WaitGroup
	stop := make(chan struct{})
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				snap := s.Snapshot()
				// 测试读取:仅验证并发安全(配 -race 运行),无错误可查。
				_, _ = snap.LookupProvider(1)
				_, _ = snap.LookupModel("m")
				_, _ = snap.LookupKey("h")
				_, _ = snap.Setting("k")
			}
		}()
	}
	for i := 0; i < 30; i++ {
		if err := s.UpsertSetting(ctx, "k", "v"); err != nil {
			t.Errorf("UpsertSetting() error = %v", err)
			break
		}
	}
	close(stop)
	wg.Wait()
}
