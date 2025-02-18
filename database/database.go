package database

import (
	"github.com/awesome-cap/hashmap"
	"github.com/ratel-online/core/log"
	modelx "github.com/ratel-online/core/model"
	"github.com/ratel-online/core/network"
	"github.com/ratel-online/core/util/async"
	"github.com/ratel-online/core/util/json"
	"github.com/ratel-online/core/util/strings"
	"github.com/ratel-online/server/consts"
	"sort"
	stringx "strings"
	"sync/atomic"
	"time"
)

var roomIds int64 = 0
var players = hashmap.New() // 存储连接过服务器的全部用户
var connPlayers = hashmap.New()
var rooms = hashmap.New()
var roomPlayers = hashmap.New()

func init() {
	async.Async(func() {
		for {
			time.Sleep(1 * time.Minute)
			rooms.Foreach(func(e *hashmap.Entry) {
				roomCancel(e.Value().(*Room))
			})
		}
	})
}

func Connected(conn *network.Conn, info *modelx.AuthInfo) *Player {
	player := &Player{
		ID:    info.ID,
		IP:    conn.IP(),
		Name:  strings.Desensitize(info.Name),
		Score: info.Score,
	}
	player.Conn(conn)                  // 初始化play对象
	players.Set(info.ID, player)       // 写入用户池
	connPlayers.Set(conn.ID(), player) // 写入连接用户池
	return player
}

func CreateRoom(creator int64, password string, playerNum int) *Room {
	room := &Room{
		ID:         atomic.AddInt64(&roomIds, 1),
		Type:       consts.GameTypeClassic,
		State:      consts.RoomStateWaiting,
		Creator:    creator,
		ActiveTime: time.Now(),
		Properties: hashmap.New(),
		MaxPlayer:  playerNum,
		Password:   password,
	}
	rooms.Set(room.ID, room)
	roomPlayers.Set(room.ID, map[int64]bool{})
	return room
}

func deleteRoom(room *Room) {
	if room != nil {
		rooms.Del(room.ID)
		roomPlayers.Del(room.ID)
		deleteGame(room.Game)
	}
}

func deleteGame(game *Game) {
	if game != nil {
		for _, state := range game.States {
			close(state)
		}
	}
}

func GetRooms() []*Room {
	list := make([]*Room, 0)
	rooms.Foreach(func(e *hashmap.Entry) {
		list = append(list, e.Value().(*Room))
	})
	sort.Slice(list, func(i, j int) bool {
		return list[i].ID < list[j].ID
	})
	return list
}

func GetRoom(roomId int64) *Room {
	return getRoom(roomId)
}

func getRoom(roomId int64) *Room {
	if v, ok := rooms.Get(roomId); ok {
		return v.(*Room)
	}
	return nil
}

func getPlayer(playerId int64) *Player {
	if v, ok := players.Get(playerId); ok {
		return v.(*Player)
	}
	return nil
}

func getRoomPlayers(roomId int64) map[int64]bool {
	if v, ok := roomPlayers.Get(roomId); ok {
		return v.(map[int64]bool)
	}
	return nil
}

// 加入房间
func JoinRoom(roomId, playerId int64, password string) error {

	// 资源检查
	player := getPlayer(playerId)
	if player == nil {
		return consts.ErrorsExist
	}
	room := getRoom(roomId)
	if room == nil {
		return consts.ErrorsRoomInvalid
	}

	// 加锁防止并发异常
	room.Lock()
	defer room.Unlock()

	room.ActiveTime = time.Now()

	// 房间状态检查
	if room.State == consts.RoomStateRunning {
		return consts.ErrorsJoinFailForRoomRunning
	}

	//房间人数检查
	if room.Players >= room.MaxPlayer {
		return consts.ErrorsRoomPlayersIsFull
	}

	// 房间密码检查
	if room.Password != password {
		return consts.ErrorsRoomPassword
	}

	playersIds := getRoomPlayers(roomId)
	if playersIds != nil {
		playersIds[playerId] = true
		room.Players++
		player.RoomID = roomId
	} else {
		deleteRoom(room)
		return consts.ErrorsRoomInvalid
	}
	return nil
}

func LeaveRoom(roomId, playerId int64) {
	room := getRoom(roomId)
	if room != nil {
		room.Lock()
		defer room.Unlock()
		leaveRoom(room, getPlayer(playerId))
	}
}

func leaveRoom(room *Room, player *Player) {
	if room == nil || player == nil {
		return
	}
	room.ActiveTime = time.Now()
	playersIds := getRoomPlayers(room.ID)
	if _, ok := playersIds[player.ID]; ok {
		room.Players--
		player.RoomID = 0
		delete(playersIds, player.ID)
		if len(playersIds) > 0 && room.Creator == player.ID {
			for k := range playersIds {
				room.Creator = k
				break
			}
		}
	}
	if len(playersIds) == 0 {
		deleteRoom(room)
	}
	return
}

func offline(roomId, playerId int64) {
	room := getRoom(roomId)
	if room != nil {
		room.Lock()
		defer room.Unlock()
		if room.State == consts.RoomStateWaiting {
			leaveRoom(room, getPlayer(playerId))
		}
		roomCancel(room)
	}
}

func roomCancel(room *Room) {
	if room.ActiveTime.Add(24 * time.Hour).Before(time.Now()) {
		log.Infof("room %d is timeout 24 hours, removed.\n", room.ID)
		deleteRoom(room)
		return
	}
	living := false
	playerIds := getRoomPlayers(room.ID)
	for id := range playerIds {
		if getPlayer(id).online {
			living = true
			break
		}
	}
	if !living {
		log.Infof("room %d is not living, removed.\n", room.ID)
		deleteRoom(room)
	}
}

func RoomPlayers(roomId int64) map[int64]bool {
	return getRoomPlayers(roomId)
}

func Broadcast(roomId int64, msg string, exclude ...int64) {
	room := getRoom(roomId)
	if room == nil {
		return
	}
	room.ActiveTime = time.Now()
	excludeSet := map[int64]bool{}
	for _, exc := range exclude {
		excludeSet[exc] = true
	}
	roomPlayers := getRoomPlayers(roomId)
	for playerId := range roomPlayers {
		if player := getPlayer(playerId); player != nil && !excludeSet[playerId] {
			_ = player.WriteString(">> " + msg)
		}
	}
}

func BroadcastChat(player *Player, msg string, exclude ...int64) {
	log.Infof("chat msg, player %s[%d] %s say: %s\n", player.Name, player.ID, player.IP, stringx.TrimSpace(msg))
	Broadcast(player.RoomID, strings.Desensitize(msg), exclude...)
}

func BroadcastObject(roomId int64, object interface{}, exclude ...int64) {
	room := getRoom(roomId)
	if room == nil {
		return
	}
	excludeSet := map[int64]bool{}
	for _, exc := range exclude {
		excludeSet[exc] = true
	}
	msg := json.Marshal(object)
	playerIds := getRoomPlayers(roomId)
	for playerId := range playerIds {
		if player := getPlayer(playerId); player != nil && !excludeSet[playerId] {
			_ = player.WriteString(string(msg))
		}
	}
}

func GetPlayer(playerId int64) *Player {
	return getPlayer(playerId)
}
