package main

import (
	"bytes"
	"encoding/binary"
	"flag"
	"log"
	"math"
	"net/http"
	"time"

	"github.com/gorilla/websocket"
)

const GCD = 1000
const KICK_CD = 4500

const (
	START_CAST = iota
	STOP_CAST  = iota
	KICK       = iota
)

const (
	CAST_TARGET = iota
	CASTED_AT   = iota
	KICK_TARGET = iota
	KICKED_AT   = iota
	HP          = iota
	ACTION_SIZE = iota
)

var port = flag.String("port", "8751", "http service port")

var upgrader = websocket.Upgrader{} // use default options

var currentState = &[4 * ACTION_SIZE]float64{}
var currentLobby = &[4]*websocket.Conn{}

func broadcast(lobby *[4]*websocket.Conn, state *[4 * ACTION_SIZE]float64) (err error) {
	wbuf := &bytes.Buffer{}
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

func now() int64 {
	return time.Now().UnixNano() / int64(time.Millisecond)
}

func calcValue(diff float64) float64 {
	return diff * (diff / 10000)
}

func joinGame(c *websocket.Conn) (err error) {
	state := currentState
	lobby := currentLobby
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

	lobby[index] = c

	start := index * ACTION_SIZE
	state[start+CAST_TARGET] = -float64(now())
	state[start+HP] = 10000

	defer func() {
		log.Println(index, "quitting lobby")
		lobby[index] = nil
		state[start+CAST_TARGET] = -float64(now())
		state[start+HP] = 0
	}()

	if playerCount >= 4 {
		log.Println("Lobby full, preparing next lobby !")
		playerCount = 0
		gameStarted = true
		currentLobby = &[4]*websocket.Conn{}
		currentState = &[4 * ACTION_SIZE]float64{}
	}

	err = c.WriteMessage(websocket.BinaryMessage, []byte{byte(index)})
	if err != nil {
		return err
	}

	// broadcast lobby update to tell others that we joined in
	err = broadcast(lobby, state)
	if err != nil {
		return err
	}

	for {
		_, message, err := c.ReadMessage()
		if err != nil {
			return err
		}

		log.Println("got message", message[0], "from player", index)

		if !gameStarted {
			for _, c := range lobby {
				if c != nil {
					playerCount++
				}
			}

			log.Println("Game didnt start yet", message[0], "from player", index)
			gameStarted = playerCount >= 4

			if !gameStarted {
				continue
			}
		}

		t := float64(now())
		lastAction := math.Max(state[start+CASTED_AT], state[start+KICKED_AT])
		isDead := state[start+HP] <= 0
		canCast := !isDead && lastAction+GCD < t
		switch message[0] {
		case START_CAST:
			if !canCast {
				continue
			}

			state[start+CAST_TARGET] = float64(message[1])
			state[start+CASTED_AT] = t
		case STOP_CAST:
			target := int(state[start+CAST_TARGET])

			if target < 0 {
				continue
			}

			targetStart := target * ACTION_SIZE
			value := calcValue(t - state[start+CASTED_AT])
			if (index > 1) == (target > 1) {
				state[targetStart+HP] = math.Min(state[targetStart+HP]+value, 10000)
			} else {
				newLife := math.Max(state[targetStart+HP]-value, 0)
				state[targetStart+HP] = newLife

				// Handle death, clear casts on target
				if newLife <= 0 {
					state[targetStart+CAST_TARGET] = -t
					for i := 0; i < 4; i++ {
						if int(state[i*ACTION_SIZE+CAST_TARGET]) == target {
							state[i*ACTION_SIZE+CAST_TARGET] = -t
						}
					}
				}
			}

			state[start+CAST_TARGET] = -t
		case KICK:
			if !canCast {
				continue
			}

			// check if kick is available
			if state[start+KICKED_AT]+KICK_CD > t {
				continue
			}

			state[start+KICKED_AT] = t
			target := message[1]
			targetStart := target * ACTION_SIZE
			if state[targetStart+CAST_TARGET] < 0 {
				state[start+KICK_TARGET] = -1
			} else {
				state[start+KICK_TARGET] = float64(target)
				// silence target if is casting
				diff := t - state[targetStart+CASTED_AT]
				decastTime := t + math.Min(diff, GCD*2)
				state[targetStart+CASTED_AT] = decastTime
				state[targetStart+CAST_TARGET] = -decastTime
			}
		}

		err = broadcast(lobby, state)
		if err != nil {
			return err
		}
	}
}

func serveWs(w http.ResponseWriter, r *http.Request) {
	c, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Print("error upgrading:", err)
		return
	}

	joinGame(c)

	defer c.Close()
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
