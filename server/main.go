package main

import (
	"flag"
	"log"
	"net/http"
	"time"
	"bytes"
	"encoding/binary"

	"github.com/gorilla/websocket"
)

const START_CAST = 0
const STOP_CAST = 1
const KICK = 2

var port = flag.String("port", "8751", "http service port")

var upgrader = websocket.Upgrader{} // use default options

var currentState = [12]float64{}
var currentLobby = [4]*websocket.Conn{}
func broadcast(lobby *[4]*websocket.Conn, state *[12]float64) (err error) {
	wbuf := new(bytes.Buffer)
	err = binary.Write(wbuf, binary.LittleEndian, state)
	if err != nil {
		return err
	}

	b := wbuf.Bytes()
	for _, c := range lobby {
		if c == nil {
			log.Println("missing connection in lobby")
			continue
		}
		err = c.WriteMessage(websocket.BinaryMessage, b)
		if err != nil {
			return err
		}
	}
	return nil
}

func now() float64 {
	return float64(time.Now().UnixNano() / int64(time.Millisecond))
}

func serveWs(w http.ResponseWriter, r *http.Request) {
	state := &currentState
	lobby := &currentLobby
	index := 0
	gameStarted := false
	playerCount := 1

	for i, c := range lobby {
		if c == nil {
			index = i
		} else {
			playerCount++
		}
	}

	if playerCount > 4 {
		http.Error(w, "Too Many Requests", http.StatusTooManyRequests)
		return 
	}

	c, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Print("upgrade:", err)
		return
	}

	lobby[index] = c
	state[index * 3] = -1

	log.Println("index", index, "playerCount", playerCount)

	if playerCount >= 4 {
		log.Println("Lobby full, preparing next lobby !")
		// TODO: gerer plusieur lobby ??
		playerCount = 0
		// currentLobby = [4]*websocket.Conn{}
		// currentState = [12]float64{}
		gameStarted = true
	}

	defer func () {
		log.Println(index, "quitting lobby")
		lobby[index] = nil
		c.Close()
	}()

	err = c.WriteMessage(websocket.BinaryMessage, []byte{byte(index)})
	if err != nil {
		log.Println("Unable to send player position:", err)
		return
	}

	if gameStarted {
		log.Println("Game started: broadcasting first state")
		err = broadcast(lobby, state)

		if err != nil {
			log.Println("write error:", err)
		}
	}

	for {
		_, message, err := c.ReadMessage()
		if err != nil {
			log.Println("read error:", err)
			return
		}

		if !gameStarted {
			for _, c := range lobby {
				if c != nil {
					playerCount++
				}
			}

			gameStarted = playerCount >= 4

			if !gameStarted {
				continue	
			}
		}

		switch message[0] {
			case START_CAST:
				state[index * 3] = float64(message[1])
				state[index * 3 + 1] = now()
			case STOP_CAST:
				state[index * 3] = -1
			case KICK:
				// TODO: handle kick
				// target := message[1]
				state[index * 3 + 2] = now()
		}

		err = broadcast(lobby, state)
		if err != nil {
			log.Println("write error:", err)
			return
		}
	}
}

func serveHome(w http.ResponseWriter, r *http.Request) {
	log.Println(r.URL)
	if r.URL.Path != "/" {
		http.Error(w, "Not found", http.StatusNotFound)
		return
	}
	if r.Method != "GET" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	http.ServeFile(w, r, "index.html")
}

func main() {
	flag.Parse()
	log.SetFlags(0) // ???
	http.HandleFunc("/ws", serveWs)
	http.HandleFunc("/", serveHome)
	log.Println("server listening on port " + *port)
	log.Fatal(http.ListenAndServe(":"+*port, nil))
}
