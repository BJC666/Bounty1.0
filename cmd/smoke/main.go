//go:build ignore

// Temporary CDP smoke-test scaffold (kept out of normal builds).
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os/exec"
	"time"

	"bounty/internal/tool/builtin"
)

func main() {
	chrome := `C:\Program Files\Google\Chrome\Application\chrome.exe`
	cmd := exec.Command(chrome, "--remote-debugging-port=9222", "--headless=new", "--no-sandbox", "--disable-gpu", "about:blank")
	if err := cmd.Start(); err != nil {
		fmt.Println("start chrome:", err)
		return
	}
	defer func() {
		cmd.Process.Kill()
		cmd.Wait()
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	// wait for devtools
	var pages []map[string]any
	for {
		resp, err := http.Get("http://localhost:9222/json")
		if err == nil {
			json.NewDecoder(resp.Body).Decode(&pages)
			resp.Body.Close()
			if len(pages) > 0 {
				break
			}
		}
		select {
		case <-ctx.Done():
			fmt.Println("devtools not ready")
			return
		case <-time.After(200 * time.Millisecond):
		}
	}
	for i, p := range pages {
		fmt.Printf("page[%d] id=%v ws=%v url=%v\n", i, p["id"], p["webSocketDebuggerUrl"], p["url"])
	}

	wsURL, _ := pages[0]["webSocketDebuggerUrl"].(string)
	conn, err := builtin.DialWSForTest(ctx, wsURL)
	if err != nil {
		fmt.Println("dial:", err)
		return
	}
	defer conn.Close()

	send := func(id int, method string, params map[string]any) {
		payload, _ := json.Marshal(map[string]any{"id": id, "method": method, "params": params})
		conn.SendTextForTest(payload)
	}

	send(1, "Page.enable", nil)
	send(2, "Runtime.enable", nil)
	send(3, "Page.navigate", map[string]any{"url": "https://example.com"})

	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		conn.Conn().SetReadDeadline(time.Now().Add(2 * time.Second))
		op, data, err := conn.ReadFrameForTest()
		if err != nil {
			fmt.Println("frame err:", err)
			if ne, ok := err.(interface{ Timeout() bool }); ok && ne.Timeout() {
				continue
			}
			break
		}
		var v map[string]any
		json.Unmarshal(data, &v)
		mid, _ := v["id"].(float64)
		if mid != 0 {
			fmt.Printf("RESP id=%v method=%v result=%v\n", mid, v["method"], v["result"])
		} else {
			fmt.Printf("EVENT method=%v params=%v\n", v["method"], v["params"])
		}
	}
}
