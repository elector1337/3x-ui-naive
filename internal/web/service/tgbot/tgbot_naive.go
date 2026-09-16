package tgbot

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/mhsanaei/3x-ui/v3/internal/logger"
	"github.com/mhsanaei/3x-ui/v3/internal/util/common"
	"github.com/mhsanaei/3x-ui/v3/internal/web/service"

	"github.com/mymmrac/telego"
	tu "github.com/mymmrac/telego/telegoutil"
)

// naive returns the bot's NaiveService, creating one for a zero-value Tgbot.
func (t *Tgbot) naive() *service.NaiveService {
	if t.naiveService == nil {
		t.naiveService = service.NewNaiveService()
	}
	return t.naiveService
}

// answerNaiveCallback handles the per-server naive management buttons
// (open menu / start / stop / restart) and refreshes the menu message.
func (t *Tgbot) answerNaiveCallback(callbackQuery *telego.CallbackQuery, chatId int64, action, arg string) {
	id, err := strconv.Atoi(arg)
	if err != nil {
		t.sendCallbackAnswerTgBot(callbackQuery.ID, err.Error())
		return
	}
	var opErr error
	switch action {
	case "naive_start":
		opErr = t.naive().Start(id)
	case "naive_stop":
		opErr = t.naive().Stop(id)
	case "naive_restart":
		opErr = t.naive().Restart(id)
	default:
		t.sendCallbackAnswerTgBot(callbackQuery.ID, t.I18nBot("tgbot.buttons.getNaive"))
	}
	if action != "naive_manage" {
		if opErr != nil {
			t.sendCallbackAnswerTgBot(callbackQuery.ID, opErr.Error())
		} else {
			t.sendCallbackAnswerTgBot(callbackQuery.ID, t.I18nBot("tgbot.answers.successfulOperation"))
		}
	}
	// Probe needs a moment to reflect the new process state.
	time.Sleep(300 * time.Millisecond)
	t.editMessageTgBot(chatId, callbackQuery.Message.GetMessageID(), t.naiveManageText(id), t.getNaiveManageKeyboard(id))
}

// getNaiveUsages retrieves and formats NaiveProxy server usage information,
// mirroring getInboundUsages. naive has no per-client stats, so this reports
// per-server up/down (kernel-sampled), quota, expiry and running state.
func (t *Tgbot) getNaiveUsages() string {
	var info strings.Builder
	servers, err := t.naive().List()
	if err != nil {
		logger.Warning("naive List for tgbot failed:", err)
		return t.I18nBot("tgbot.answers.getNaiveFailed")
	}
	if len(servers) == 0 {
		return ""
	}
	info.WriteString(t.I18nBot("tgbot.messages.naiveHeader"))
	for _, srv := range servers {
		state := t.I18nBot("tgbot.messages.naiveStopped")
		if t.naive().Status(srv.Id).Running {
			state = t.I18nBot("tgbot.messages.naiveRunning")
		}
		info.WriteString(t.I18nBot("tgbot.messages.naiveServer", "Remark=="+srv.Remark, "State=="+state))
		info.WriteString(t.I18nBot("tgbot.messages.port", "Port=="+strconv.Itoa(srv.Port)))
		info.WriteString(t.I18nBot("tgbot.messages.traffic", "Total=="+common.FormatTraffic(srv.Up+srv.Down), "Upload=="+common.FormatTraffic(srv.Up), "Download=="+common.FormatTraffic(srv.Down)))
		if srv.ExpiryTime == 0 {
			info.WriteString(t.I18nBot("tgbot.messages.expire", "Time=="+t.I18nBot("tgbot.unlimited")))
		} else {
			info.WriteString(t.I18nBot("tgbot.messages.expire", "Time=="+time.Unix((srv.ExpiryTime/1000), 0).Format("2006-01-02 15:04:05")))
		}
		info.WriteString("\r\n")
	}
	return info.String()
}

// getNaiveServersKeyboard builds a keyboard with one button per naive server
// (remark + running/stopped icon) that opens its management menu.
func (t *Tgbot) getNaiveServersKeyboard() (*telego.InlineKeyboardMarkup, error) {
	servers, err := t.naive().List()
	if err != nil {
		logger.Warning("naive List for tgbot keyboard failed:", err)
		return nil, errors.New(t.I18nBot("tgbot.answers.getNaiveFailed"))
	}
	if len(servers) == 0 {
		return nil, errors.New(t.I18nBot("tgbot.answers.noNaive"))
	}
	var buttons []telego.InlineKeyboardButton
	for _, srv := range servers {
		icon := "🔴"
		if t.naive().Status(srv.Id).Running {
			icon = "🟢"
		}
		label := fmt.Sprintf("%s %s", icon, firstNonEmpty(srv.Remark, fmt.Sprintf("naive-%d", srv.Id)))
		buttons = append(buttons, tu.InlineKeyboardButton(label).WithCallbackData(t.encodeQuery(fmt.Sprintf("naive_manage %d", srv.Id))))
	}
	cols := 1
	if len(buttons) >= 6 {
		cols = 2
	}
	return tu.InlineKeyboardGrid(tu.InlineKeyboardCols(cols, buttons...)), nil
}

// getNaiveManageKeyboard builds the start/stop/restart menu for one server.
func (t *Tgbot) getNaiveManageKeyboard(id int) *telego.InlineKeyboardMarkup {
	running := t.naive().Status(id).Running
	var firstRow []telego.InlineKeyboardButton
	if running {
		firstRow = append(firstRow, tu.InlineKeyboardButton(t.I18nBot("tgbot.buttons.naiveStop")).WithCallbackData(t.encodeQuery(fmt.Sprintf("naive_stop %d", id))))
	} else {
		firstRow = append(firstRow, tu.InlineKeyboardButton(t.I18nBot("tgbot.buttons.naiveStart")).WithCallbackData(t.encodeQuery(fmt.Sprintf("naive_start %d", id))))
	}
	firstRow = append(firstRow, tu.InlineKeyboardButton(t.I18nBot("tgbot.buttons.naiveRestart")).WithCallbackData(t.encodeQuery(fmt.Sprintf("naive_restart %d", id))))
	return tu.InlineKeyboard(
		tu.InlineKeyboardRow(firstRow...),
		tu.InlineKeyboardRow(tu.InlineKeyboardButton(t.I18nBot("tgbot.buttons.getNaive")).WithCallbackData(t.encodeQuery("naive"))),
	)
}

// naiveManageText renders a one-server status line for the management menu.
func (t *Tgbot) naiveManageText(id int) string {
	srv, err := t.naive().Get(id)
	if err != nil {
		return t.I18nBot("tgbot.answers.getNaiveFailed")
	}
	state := t.I18nBot("tgbot.messages.naiveStopped")
	if t.naive().Status(srv.Id).Running {
		state = t.I18nBot("tgbot.messages.naiveRunning")
	}
	var b strings.Builder
	b.WriteString(t.I18nBot("tgbot.messages.naiveServer", "Remark=="+firstNonEmpty(srv.Remark, fmt.Sprintf("naive-%d", srv.Id)), "State=="+state))
	b.WriteString(t.I18nBot("tgbot.messages.port", "Port=="+strconv.Itoa(srv.Port)))
	b.WriteString(t.I18nBot("tgbot.messages.traffic", "Total=="+common.FormatTraffic(srv.Up+srv.Down), "Upload=="+common.FormatTraffic(srv.Up), "Download=="+common.FormatTraffic(srv.Down)))
	return b.String()
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
