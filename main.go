package main

import (
	"bufio"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/miscord-dev/palog/pkg/palrcon"
)

type MemInfo struct {
	TotalMemory int
	UsedMemory  int
}

var (
	rconEndpoint = os.Getenv("RCON_ENDPOINT")
	rconPassword = os.Getenv("RCON_PASSWORD")

	intervalRaw = os.Getenv("INTERVAL")
	interval    time.Duration

	timeoutRaw = os.Getenv("TIMEOUT")
	timeout    time.Duration

	uconvLatin = os.Getenv("UCONV_LATIN") != "false"
)

func init() {
	var err error

	if timeoutRaw == "" {
		timeoutRaw = "1s"
	}

	timeout, err = time.ParseDuration(timeoutRaw)
	if err != nil {
		slog.Error("failed to parse timeout", "error", err)
		os.Exit(1)
	}

	if intervalRaw == "" {
		intervalRaw = "5s"
	}

	interval, err = time.ParseDuration(intervalRaw)

	if err != nil {
		slog.Error("failed to parse interval", "error", err)
		os.Exit(1)
	}
}

func runMecab(s string) string {

	kanaConv := unicode.SpecialCase{
		unicode.CaseRange{
			Lo: 0x30a1, // Lo: ァ
			Hi: 0x30f3, // Hi: ン
			Delta: [unicode.MaxCase]rune{
				0x3041 - 0x30a1,
				0x3041 - 0x30a1,
				0x3041 - 0x30a1,
			},
		},
	}

	var reading strings.Builder

	cmd := exec.Command("mecab")
	stdin, err := cmd.StdinPipe()

	if err != nil {
		slog.Error("failed to run mecab", "error", err)
		return s
	}

	io.WriteString(stdin, s)
	stdin.Close()

	out, err := cmd.Output()

	if err != nil {
		slog.Error("failed to run mecab", "error", err)
		return s
	}

	scanner := bufio.NewScanner(strings.NewReader(string(out)))
	for scanner.Scan() {
		line := scanner.Text()

		if len(line) == 0 {
			continue
		}

		word := regexp.MustCompile(`\t+`).Split(line, -1)

		if len(word) < 2 {
			continue
		}

		fields := strings.Split(word[1], ",")

		if len(fields) < 8 || fields[7] == "" || fields[7] == "*" {
			reading.WriteString(word[0])
		} else {
			reading.WriteString(strings.ToUpperSpecial(kanaConv, fields[7]))
		}

	}

	return reading.String()
}

func runUconvLatin(s string) string {
	var out strings.Builder
	cmd := exec.Command("uconv", "-x", "latin")
	cmd.Stdin = strings.NewReader(s)
	cmd.Stderr = os.Stderr
	cmd.Stdout = &out

	err := cmd.Run()
	if err != nil {
		slog.Error("failed to run uconv", "error", err)
		return s
	}

	return out.String()
}

func runFree() MemInfo {
	var out strings.Builder
	cmd := exec.Command("free", "-b")
	cmd.Stderr = os.Stderr
	cmd.Stdout = &out

	err := cmd.Run()
	if err != nil {
		slog.Error("failed to run free", "error", err)
		return MemInfo{
			TotalMemory: 0,
			UsedMemory: 0,
		}
	}

	line := strings.Split(out.String(), "\n")[1] // skip header

	if len(line) == 0 {
		slog.Error("failed to run free", "len(line)", len(line))
		return MemInfo{
			TotalMemory: 0,
			UsedMemory: 0,
		}
	}

	fields := regexp.MustCompile(`\s+`).Split(line, -1)
	if len(fields) < 3 {
		slog.Error("failed to run free", "len(fields)", len(fields))
		return MemInfo{
			TotalMemory: 0,
			UsedMemory: 0,
		}
	}

	intTotal, err := strconv.Atoi(fields[1]);
	if err != nil {
		slog.Error("failed to run free", "error", err)
		return MemInfo{
			TotalMemory: 0,
			UsedMemory: 0,
		}
	}
	intUsed, err := strconv.Atoi(fields[2]);
	if err != nil {
		slog.Error("failed to run free", "error", err)
		return MemInfo{
			TotalMemory: intTotal,
			UsedMemory: 0,
		}
	}

	return MemInfo{
		TotalMemory: intTotal,
		UsedMemory: intUsed,
	}
}

func escapeString(s string) string {

	s = strings.ReplaceAll(s, `\x00`, "")
	if uconvLatin {
		s = runMecab(s)
		s = runUconvLatin(s)
	}
	s = strings.ReplaceAll(s, " ", "_")
	s = strings.TrimSpace(s)

	runes := []rune(s)
	for i := range runes {
		b := []byte(string(runes[i]))

		if len(b) != 1 {
			runes[i] = '?'
		}
	}

	return string(runes)
}

const notificationThreshold = 3

func main() {
	palRCON := palrcon.NewPalRCON(rconEndpoint, rconPassword)
	palRCON.SetTimeout(timeout)

	var prev map[string]palrcon.Player
	var prevSub map[string]palrcon.Player
	var prevSub2 map[string]palrcon.Player
	var onlinePlayers map[string]palrcon.Player
	var playerAppearances = make(map[string]int)
	var playerDisappearances = make(map[string]int)

	makeMap := func(players []palrcon.Player) map[string]palrcon.Player {
		m := make(map[string]palrcon.Player)

		for _, player := range players {
			if player.Name == "" || player.SteamID == "00000000" {
				continue
			}

			m[player.Name] = player
		}

		return m
	}
	makeSubMap := func(players []palrcon.Player) map[string]palrcon.Player {
		m := make(map[string]palrcon.Player)

		for _, player := range players {
			if player.PlayerUID == "00000000" || player.SteamID == "00000000" || len(player.PlayerUID) < 9 {
				continue
			}

			m[player.PlayerUID] = player
		}

		return m
	}

	makeSub2Map := func(players []palrcon.Player) map[string]palrcon.Player {
		m := make(map[string]palrcon.Player)

		for _, player := range players {
			if player.SteamID == "00000000" || strings.Contains(player.SteamID, `\x00`) || len(player.PlayerUID) < 12 {
				continue
			}

			m[player.SteamID] = player
		}

		return m
	}

	retriedBoarcast := func(message string) error {
		message = escapeString(message)

		var err error
		for i := 0; i < 10; i++ {
			err = palRCON.Broadcast(message)
			if err != nil {
				slog.Error("failed to broadcast", "error", err)
				continue
			} else {
				slog.Info("send broadcast", "broadcast", message)
			}
			return nil
		}

		return fmt.Errorf("failed to broadcast: %w", err)
	}

	noticeFlg := false

	for {
		{
			players, err := palRCON.GetPlayers()

			if err != nil {
				slog.Error("failed to get players", "error", err)
				goto NEXT
			}

			slog.Debug("Current players", "players", players)

			playersMap := makeMap(players)
			playersSubMap := makeSubMap(players)
			playersSub2Map := makeSub2Map(players)

			if prev == nil {
				onlinePlayers = playersMap
				prev = playersMap
				prevSub = playersSubMap
				prevSub2 = playersSub2Map
				goto NEXT
			}

			jst, err := time.LoadLocation("Asia/Tokyo")

			if err != nil {
				fmt.Println("Error loading time zone:", err)
				return
			}

			t := time.Now().In(jst)

			const layout = "15:04"

			for playerName, player := range playersMap {
				if _, ok := onlinePlayers[playerName]; !ok {
					// 新しく参加したプレイヤー
			
					// 既に同じPlayerUIDが存在するか確認
					if existingPlayer, exists := prevSub[player.PlayerUID]; exists {
						// 同一人物として扱う
						playerName = existingPlayer.Name
					}
			
					// 既に同じSteamIDが存在するか確認
					if existingPlayer, exists := prevSub2[player.SteamID]; exists {
						// 同一人物として扱う
						playerName = existingPlayer.Name
					}
					
					// 同じようなタイミングで退出しているプレイヤーがいる場合、ニックネームバグの可能性が高い
					if playerAppearances[playerName] == 2 {
						for pn, count := range playerDisappearances {
							if count == 2 {
								//通知せずに入れ替え
								slog.Info("nameChange:" + pn, "newName", player.Name)
								delete(playerAppearances, playerName)
								delete(playerDisappearances, pn)
								delete(onlinePlayers, pn)
								onlinePlayers[player.Name] = player
								continue
							}
						}
					}

					if _, ok := onlinePlayers[playerName]; ok {
						continue
					}
					
					delete(playerDisappearances, playerName)
					
					playerAppearances[playerName]++
					slog.Info("playerAppearances:" + playerName, "count", playerAppearances[playerName])
					if playerAppearances[playerName] >= notificationThreshold {
						delete(playerAppearances, playerName)
						slog.Info("Player joined", "player", player)
						onlinePlayers[player.Name] = player
						err := retriedBoarcast(fmt.Sprintf("[%s]player-joined:%s(%d/32)", t.Format(layout), player.Name, len(onlinePlayers)))
						if err != nil {
							slog.Error("failed to broadcast", "error", err)
						}
					}
				}
			}
			
			for playerName, player := range onlinePlayers {
				if _, ok := playersMap[player.Name]; !ok {
					// 退出したプレイヤー
			
					// 既に同じPlayerUIDが存在するか確認
					if existingPlayer, exists := playersSubMap[player.PlayerUID]; exists {
						// 同一人物として扱う
						playerName = existingPlayer.Name
					}
			
					// 既に同じSteamIDが存在するか確認
					if existingPlayer, exists := playersSub2Map[player.SteamID]; exists {
						// 同一人物として扱う
						playerName = existingPlayer.Name
					}
					
					if _, ok := playersMap[playerName]; ok {
						continue
					}
					
					delete(playerAppearances, playerName)
					
					playerDisappearances[playerName]++
					slog.Info("playerDisappearances:" + playerName, "count", playerDisappearances[playerName])
					
					if playerDisappearances[playerName] >= notificationThreshold {
						delete(playerDisappearances, playerName)
						delete(onlinePlayers, playerName)
						slog.Info("Player left", "player", player)
						err := retriedBoarcast(fmt.Sprintf("[%s]player-left:%s(%d/32)", t.Format(layout), playerName, len(onlinePlayers)))
						if err != nil {
							slog.Error("failed to broadcast", "error", err)
						}
					}
				}
			}

			prev = playersMap
			prevSub = playersSubMap
			prevSub2 = playersSub2Map

			const layouth = "15"
			const layoutm = "04"

			if t.Format(layoutm) == "00" || t.Format(layoutm) == "30" {
				if !noticeFlg {
					memInfo := runFree();
					var playerNames []string 
					for onlinePlayer := range onlinePlayers {
						playerNames =  append(playerNames, onlinePlayer)
					}
					playerNamesText := "Online:" + strings.Join(playerNames, ",")
					if t.Format(layouth) == "00" && t.Format(layoutm) == "00" {
						slog.Info("mem", "used", memInfo.UsedMemory)
						slog.Info("mem", "total", memInfo.TotalMemory)
						const layoutd = "01/02_15:04"
						err := retriedBoarcast(fmt.Sprintf("---%s---(%d/32)<Mem:%.1f%%>", t.Format(layoutd), len(onlinePlayers), float64(memInfo.UsedMemory)*float64(1000)/float64(memInfo.TotalMemory)/float64(10)))
						if err != nil {
							slog.Error("failed to broadcast", "error", err)
							continue
						}
					} else {
						slog.Info("mem", "used", memInfo.UsedMemory)
						slog.Info("mem", "total", memInfo.TotalMemory)
						err := retriedBoarcast(fmt.Sprintf("---%s---(%d/32)<Mem:%.1f%%>", t.Format(layout), len(onlinePlayers), float64(memInfo.UsedMemory)*float64(1000)/float64(memInfo.TotalMemory)/float64(10)))
						if err != nil {
							slog.Error("failed to broadcast", "error", err)
							continue
						}
					}
					err := retriedBoarcast(string(playerNamesText))
					if err != nil {
						slog.Error("failed to broadcast", "error", err)
						continue
					}
					noticeFlg = true
				}
			} else {
				noticeFlg = false
			}
		}
	NEXT:
		time.Sleep(interval)
	}
}
