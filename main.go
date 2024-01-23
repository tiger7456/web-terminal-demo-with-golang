package main

import (
	"embed"
	"encoding/json"
	"github.com/gorilla/websocket"
	"log"
	"net/http"
	"os/exec"
	"syscall"

	"github.com/creack/pty"
	"github.com/olahol/melody"
)

//go:embed index.html index2.html node_modules/xterm/css/xterm.css node_modules/xterm/lib/xterm.js
var content embed.FS
var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

func serveWs(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println(err)
		return
	}
	defer conn.Close()

	c := exec.Command("bash")
	ptmx, err := pty.Start(c)
	if err != nil {
		log.Println(err)
		return
	}

	defer func() {
		ptmx.Close()
		c.Process.Signal(syscall.SIGKILL)
	}()

	go func() {
		for {
			_, message, err := conn.ReadMessage()
			if err != nil {
				log.Println(err)
				return
			}
			log.Printf("Received: %s\n", message)
			// 尝试解析消息为 JSON
			var msg map[string]interface{}
			if json.Unmarshal(message, &msg) == nil {
				// 成功解析为 JSON，检查是否有 resize 动作
				if action, ok := msg["action"].(string); ok && action == "resize" {
					// 调整 pty 大小
					if cols, ok := msg["cols"].(float64); ok {
						if rows, ok := msg["rows"].(float64); ok {
							wsCols := uint16(cols)
							wsRows := uint16(rows)
							err := pty.Setsize(ptmx, &pty.Winsize{Cols: wsCols, Rows: wsRows})
							if err != nil {
								log.Println(err)
							}
						}
					}
				}
			} else {
				// 消息不是 JSON，假定为普通输入
				ptmx.Write(message)
			}
		}
	}()

	for {
		buf := make([]byte, 1024)
		n, err := ptmx.Read(buf)
		if err != nil {
			log.Println(err)
			return
		}
		conn.WriteMessage(websocket.TextMessage, buf[:n])
	}
}
func main() {
	c := exec.Command("./runCli") // 系统默认shell交互程序
	f, err := pty.Start(c)        // pty用于调用系统自带的虚拟终端
	if err != nil {
		panic(err)
	}

	m := melody.New() // melody用于实现WebSocket功能

	go func() { // 处理来自虚拟终端的消息
		for {
			buf := make([]byte, 1024)
			read, err := f.Read(buf)
			if err != nil {
				return
			}
			// fmt.Println("f.Read: ", string(buf[:read]))
			m.Broadcast(buf[:read]) // 将数据发送给网页
		}
	}()

	m.HandleMessage(func(s *melody.Session, msg []byte) { // 处理来自WebSocket的消息
		// fmt.Println("m.HandleMessage: ", string(msg))
		f.Write(msg) // 将消息写到虚拟终端
	})

	http.HandleFunc("/webterminal", func(w http.ResponseWriter, r *http.Request) {
		m.HandleRequest(w, r) // 访问 /webterminal 时将转交给melody处理
	})

	fs := http.FileServer(http.FS(content))
	http.Handle("/", http.StripPrefix("/", fs)) // 设置静态文件服务

	http.HandleFunc("/ws", serveWs)

	http.ListenAndServe("0.0.0.0:22333", nil) // 启动服务器，访问 http://本机(服务器)IP地址:22333/ 进行测试
}
