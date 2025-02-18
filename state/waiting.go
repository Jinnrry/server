package state

import (
	"bytes"
	"fmt"
	"github.com/awesome-cap/hashmap"
	"github.com/ratel-online/server/consts"
	"github.com/ratel-online/server/database"
	"github.com/ratel-online/server/rule"
	"github.com/ratel-online/server/state/game"
	"strconv"
	"strings"
	"time"
)

type waiting struct{}

func (s *waiting) Next(player *database.Player) (consts.StateID, error) {
	room := database.GetRoom(player.RoomID)
	if room == nil {
		return 0, consts.ErrorsExist
	}
	if room.Type == consts.GameTypeLaiZi {
		room.SetProperty(consts.RoomPropsLaiZi, true)
	} else if room.Type == consts.GameTypeSkill {
		room.SetProperty(consts.RoomPropsLaiZi, true)
		room.SetProperty(consts.RoomPropsDotShuffle, true)
		room.SetProperty(consts.RoomPropsSkill, true)
	}
	access, err := waitingForStart(player, room)
	if err != nil {
		return 0, err
	}
	if access {
		return consts.StateGame, nil
	}
	return s.Exit(player), nil
}

func (*waiting) Exit(player *database.Player) consts.StateID {
	room := database.GetRoom(player.RoomID)
	if room != nil {
		isOwner := room.Creator == player.ID
		database.LeaveRoom(room.ID, player.ID)
		database.Broadcast(room.ID, fmt.Sprintf("%s exited room! room current has %d players\n", player.Name, room.Players))
		if isOwner {
			newOwner := database.GetPlayer(room.Creator)
			database.Broadcast(room.ID, fmt.Sprintf("%s become new owner\n", newOwner.Name))
		}
	}
	return consts.StateHome
}

func waitingForStart(player *database.Player, room *database.Room) (bool, error) {
	access := false
	player.StartTransaction()
	defer player.StopTransaction()
	for {
		signal, err := player.AskForStringWithoutTransaction(time.Second)
		if err != nil && err != consts.ErrorsTimeout {
			return access, err
		}
		if room.State == consts.RoomStateRunning {
			access = true
			break
		}
		signal = strings.ToLower(signal)
		if signal == "ls" || signal == "v" {
			viewRoomPlayers(room, player)
		} else if (signal == "start" || signal == "s") && room.Creator == player.ID && room.Players > 1 {
			access = true
			room.Lock()
			room.Game, err = initGame(room)
			if err != nil {
				_ = player.WriteError(err)
				return access, err
			}
			room.State = consts.RoomStateRunning
			room.Unlock()
			break
		} else if strings.HasPrefix(signal, "set ") && room.Creator == player.ID {
			tags := strings.Split(signal, " ")
			if len(tags) == 3 {
				switch strings.TrimSpace(tags[1]) {
				case consts.RoomPropsPassword:
					pwd := strings.TrimSpace(tags[2])

					// 不允许10位以上的密码，防止恶意输入超长文本占满服务器资源
					if len(pwd) > 10 {
						pwd = ""
						buf := bytes.Buffer{}
						buf.WriteString("Your password is too long, must less 10 charts.  \n")
						_ = player.WriteString(buf.String())
					}

					room.Password = pwd
				case consts.RoomPropsPlayerNum:
					playerNum, err := strconv.Atoi(strings.TrimSpace(tags[2]))
					if err == nil && playerNum > 1 && playerNum <= consts.MaxPlayers {
						room.MaxPlayer = playerNum
					}
				default:
					room.SetProperty(tags[1], tags[2] == "on")
				}
				continue
			}
			database.BroadcastChat(player, fmt.Sprintf("%s say: %s\n", player.Name, signal))
		} else if len(signal) > 0 {
			database.BroadcastChat(player, fmt.Sprintf("%s say: %s\n", player.Name, signal))
		}
	}
	return access, nil
}

func viewRoomPlayers(room *database.Room, currPlayer *database.Player) {
	buf := bytes.Buffer{}
	buf.WriteString(fmt.Sprintf("Room ID: %d\n", room.ID))
	buf.WriteString(fmt.Sprintf("%-20s%-10s%-10s\n", "Name", "Score", "Title"))
	for playerId := range database.RoomPlayers(room.ID) {
		title := "player"
		if playerId == room.Creator {
			title = "owner"
		}
		player := database.GetPlayer(playerId)
		buf.WriteString(fmt.Sprintf("%-20s%-10d%-10s\n", player.Name, player.Score, title))
	}
	buf.WriteString("Properties: ")
	room.Properties.Foreach(func(e *hashmap.Entry) {
		if e.Value().(bool) {
			buf.WriteString(e.Key().(string) + " ")
		}
	})
	buf.WriteString("\n")
	_ = currPlayer.WriteString(buf.String())
}

func initGame(room *database.Room) (*database.Game, error) {
	rules := rule.LandlordRules
	if room.GetProperty(consts.RoomPropsSkill) {
		rules = rule.TeamRules
	}
	return game.InitGame(room, rules)
}
