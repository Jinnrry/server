package state

import (
	"bytes"
	"github.com/ratel-online/server/consts"
	"github.com/ratel-online/server/service"
)

type home struct{}

func (*home) Next(player *service.Player) (consts.StateID, error) {
	buf := bytes.Buffer{}
	buf.WriteString("1.Join\n")
	buf.WriteString("2.New\n")
	err := player.WriteString(buf.String())
	if err != nil {
		return 0, player.WriteError(err)
	}
	selected, err := player.AskForInt()
	if err != nil {
		return 0, player.WriteError(err)
	}
	if selected == 1 {
		return consts.StateJoin, nil
	} else if selected == 2 {
		return consts.StateNew, nil
	} else if selected == 3 {
		return consts.StateSetting, nil
	}
	return 0, player.WriteError(consts.ErrorsInputInvalid)
}

func (*home) Exit(player *service.Player) consts.StateID {
	return 0
}
