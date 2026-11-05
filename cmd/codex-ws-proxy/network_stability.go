package main

import (
	"context"
	"fmt"
	"io"
	"math"
	"slices"
	"time"

	"github.com/QuantumNous/new-api/pkg/networktest"
	"github.com/gorilla/websocket"
)

type stabilitySummary struct {
	started      time.Time
	lastSuccess  time.Time
	rtts         []time.Duration
	attempts     int
	failed       int
	interrupted  int
	failureStage string
	probeError   error
}

type stabilityReply struct {
	kind    int
	payload []byte
	err     error
}

func runNetworkStability(parent context.Context, conn *websocket.Conn, duration, timeout time.Duration, output io.Writer) error {
	ctx, cancel := context.WithTimeout(parent, duration)
	defer cancel()
	deadline, _ := ctx.Deadline()
	summary := stabilitySummary{started: time.Now()}
	// 到达总时长或收到停止信号时，解除当前阻塞读写，并尽量发送正常关闭帧。
	stopClose := context.AfterFunc(ctx, func() {
		_ = conn.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, "稳定性测试结束"), time.Now().Add(100*time.Millisecond))
		_ = conn.Close()
	})
	defer stopClose()

	err := networktest.AwaitStabilityReady(conn, min(timeout, time.Until(deadline)))
	if err != nil && parent.Err() == nil && time.Now().Before(deadline) {
		summary.failureStage = "服务端准备"
		summary.probeError = err
	}
	replies := make(chan stabilityReply)
	if err == nil {
		// 唯一的读取协程也在探测间隔中工作，处理 Ping/Pong 并及时观察关闭帧。
		err = conn.SetReadDeadline(time.Time{})
		conn.SetReadLimit(networktest.StabilityBytes)
		readerCtx, stopReader := context.WithCancel(ctx)
		readerDone := make(chan struct{})
		go func() {
			defer close(readerDone)
			for {
				kind, payload, readErr := conn.ReadMessage()
				select {
				case replies <- stabilityReply{kind, payload, readErr}:
				case <-readerCtx.Done():
					return
				}
				if readErr != nil {
					return
				}
			}
		}()
		defer func() {
			stopReader()
			_ = conn.Close()
			<-readerDone
		}()
		if err != nil {
			summary.failureStage = "服务端准备"
			summary.probeError = err
		}
	}
	for err == nil && ctx.Err() == nil && time.Now().Before(deadline) {
		summary.attempts++
		phase := "发送探测"
		var payload []byte
		var started time.Time
		payload, started, err = networktest.WriteStabilityProbe(conn, uint64(summary.attempts), min(timeout, time.Until(deadline)))
		if err == nil {
			phase = "等待回包"
			timer := time.NewTimer(min(time.Until(started.Add(timeout)), time.Until(deadline)))
			select {
			case reply := <-replies:
				err = reply.err
				if err == nil {
					phase = "回包校验"
					err = networktest.ValidateStabilityReply(reply.kind, reply.payload, payload)
				}
			case <-ctx.Done():
				err = ctx.Err()
			case <-timer.C:
				err = context.DeadlineExceeded
			}
			timer.Stop()
		}
		if err != nil {
			if ctx.Err() != nil || !time.Now().Before(deadline) {
				// 测试主动结束导致的未完成样本不冒充网络故障。
				summary.interrupted++
			} else {
				summary.failed++
				summary.failureStage = phase
				summary.probeError = err
			}
			break
		}
		rtt := time.Since(started)
		summary.rtts = append(summary.rtts, rtt)
		summary.lastSuccess = time.Now()
		fmt.Fprintf(output, "[%s] 探测 #%d 成功：往返 %s，上传和回包各 %d 字节，内容校验通过\n", summary.lastSuccess.Format("15:04:05"), summary.attempts, rtt.Round(time.Microsecond), networktest.StabilityBytes)
		timer := time.NewTimer(networktest.StabilityInterval)
		select {
		case <-ctx.Done():
		case <-timer.C:
		case reply := <-replies:
			if ctx.Err() == nil && time.Now().Before(deadline) {
				err = reply.err
				if err == nil {
					err = fmt.Errorf("探测间隔内收到非预期的数据消息")
				}
				summary.failureStage = "间隔等待"
				summary.probeError = err
			}
		}
		timer.Stop()
	}
	ended := time.Now()
	reason := "达到设定时长"
	if parent.Err() != nil {
		reason = "收到停止信号，提前结束"
	} else if summary.probeError != nil {
		reason = "探测或连接异常，停止测试；没有重连"
	}
	if summary.probeError == nil {
		_ = conn.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, "稳定性测试结束"), time.Now().Add(100*time.Millisecond))
	}
	summary.write(output, duration, ended, reason)
	if parent.Err() != nil {
		return fmt.Errorf("稳定性测试已中止: %w", parent.Err())
	}
	if summary.probeError != nil {
		return fmt.Errorf("稳定性测试在%s阶段失败: %w", summary.failureStage, summary.probeError)
	}
	if len(summary.rtts) == 0 {
		return fmt.Errorf("没有完成任何探测，无法评价网络稳定性")
	}
	return nil
}

func (s stabilitySummary) write(output io.Writer, requested time.Duration, ended time.Time, reason string) {
	fmt.Fprintln(output, "\n长连接稳定性汇总")
	fmt.Fprintf(output, "结束原因：%s\n观察区间：%s ～ %s\n计划时长：%s；实际观察：%s；WebSocket 连接：1 条；重连：0 次\n", reason, s.started.Format(time.RFC3339), ended.Format(time.RFC3339), requested, ended.Sub(s.started).Round(time.Millisecond))
	fmt.Fprintf(output, "探测次数：%d；成功：%d；失败：%d；结束时中止：%d\n", s.attempts, len(s.rtts), s.failed, s.interrupted)
	if s.failureStage == "间隔等待" {
		fmt.Fprintln(output, "连接在探测间隔中发生异常；已完成的探测成功率不代表连接维持成功。")
	}
	completed := len(s.rtts) + s.failed
	if completed > 0 {
		fmt.Fprintf(output, "已完成探测成功率：%.2f%%（不含主动结束时中止的样本）\n", float64(len(s.rtts))*100/float64(completed))
	} else {
		fmt.Fprintln(output, "已完成探测成功率：无样本")
	}
	if len(s.rtts) > 0 {
		var total, maxChange time.Duration
		for i, rtt := range s.rtts {
			total += rtt
			if i > 0 {
				change := time.Duration(math.Abs(float64(rtt - s.rtts[i-1])))
				maxChange = max(maxChange, change)
			}
		}
		sorted := slices.Clone(s.rtts)
		slices.Sort(sorted)
		p95 := sorted[(len(sorted)*95+99)/100-1]
		fmt.Fprintf(output, "往返 RTT：最小 %s；平均 %s；P95 %s；最大 %s\n", sorted[0].Round(time.Microsecond), (total / time.Duration(len(sorted))).Round(time.Microsecond), p95.Round(time.Microsecond), sorted[len(sorted)-1].Round(time.Microsecond))
		if len(s.rtts) > 1 {
			fmt.Fprintf(output, "相邻探测 RTT 最大变化：%s\n", maxChange.Round(time.Microsecond))
		}
		fmt.Fprintf(output, "最后成功确认：%s；距测试结束：%s\n", s.lastSuccess.Format(time.RFC3339), ended.Sub(s.lastSuccess).Round(time.Millisecond))
		fmt.Fprintf(output, "已校验数据：上传 %d 字节，回传 %d 字节（不含未完成样本及协议开销）\n", len(s.rtts)*networktest.StabilityBytes, len(s.rtts)*networktest.StabilityBytes)
	}
	if s.probeError != nil {
		fmt.Fprintf(output, "失败阶段：%s；错误：%v\n", s.failureStage, s.probeError)
	}
	fmt.Fprintln(output, "统计反映同一 WebSocket 上的应用层探测结果；RTT 包含服务端处理，不等于单向延迟或网络丢包率，也不包含 CPA/模型上游链路。")
}
