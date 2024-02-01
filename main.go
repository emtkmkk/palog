package main

import (
	"fmt"
	"log/slog"
	"io"
	"os"
	"os/exec"
	"strings"
	"unicode"
	"time"
	"regexp"
	"bufio"

	"github.com/miscord-dev/palog/pkg/palrcon"
)

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
	var reading strings.Builder

	slog.Info("mecab", "in", s)
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

	slog.Info("mecab", "out", err)
	scanner := bufio.NewScanner(strings.NewReader(string(out)))
	for scanner.Scan() {
		line := scanner.Text()
		
		slog.Info("mecab", "line", line)

		if len(line) == 0 {
			continue
		}

		word := regexp.MustCompile("\\t+").Split(line, -1)
		
		if len(word) < 2 {
			continue
		}
		
		slog.Info("mecab", "word1", word[1])

		fields := strings.Split(word[1], ",")

		if len(fields) < 8 || fields[7] == "" || fields[7] == "*" {
			slog.Info("mecab", "word", word[0])
			reading.WriteString(word[0])
		} else {
			slog.Info("mecab", "word", fields[7])
			reading.WriteString(fields[7])
		}

	}

	kanaConv := unicode.SpecialCase{
	    unicode.CaseRange{
	        0x30a1, // Lo: ァ
	        0x30f3, // Hi: ン
	        [unicode.MaxCase]rune{
	            0x3041 - 0x30a1,
	            0x3041 - 0x30a1,
	            0x3041 - 0x30a1,
	        },
	    },
	}

	return strings.ToUpperSpecial(kanaConv, reading.String())
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

func escapeString(s string) string {
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
			runes[i] = '*'
		}
	}

	return string(runes)
}

func main() {
	palRCON := palrcon.NewPalRCON(rconEndpoint, rconPassword)
	palRCON.SetTimeout(timeout)

	var prev map[string]palrcon.Player

	makeMap := func(players []palrcon.Player) map[string]palrcon.Player {
		m := make(map[string]palrcon.Player)

		for _, player := range players {
			if player.PlayerUID == "00000000" {
				continue
			}

			m[player.Name + player.PlayerUID[:2]] = player
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

			if prev == nil {
				prev = playersMap
				goto NEXT
			}
   			t := time.Now()
    			const layout = "15:04"

			for _, player := range playersMap {
				if _, ok := prev[player.Name + player.PlayerUID[:2]]; !ok {
					if player.Name != "" {
						err := retriedBoarcast(fmt.Sprintf("[%s]player-joined:%s(%d/32)", t.Format(layout), player.Name, len(playersMap)))
						if err != nil {
							slog.Error("failed to broadcast", "error", err)
							continue
						}
					} else {
						err := retriedBoarcast(fmt.Sprintf("[%s]player-joined:%s(%d/32)", t.Format(layout), player.PlayerUID, len(playersMap)))
						if err != nil {
							slog.Error("failed to broadcast", "error", err)
							continue
						}
					}

					slog.Info("Player joined", "player", player)
				}
			}
			for _, player := range prev {
				if _, ok := playersMap[player.Name + player.PlayerUID[:2]]; !ok {
					slog.Info("Player left", "player", player)

					err := retriedBoarcast(fmt.Sprintf("[%s]player-left:%s(%d/32)", t.Format(layout), player.Name, len(playersMap)))
					if err != nil {
						slog.Error("failed to broadcast", "error", err)
					}
				}
			}

			prev = playersMap
			
			const layoutm = "04"
			
			if t.Format(layoutm) == "00" {
				if !noticeFlg {
					err := retriedBoarcast(fmt.Sprintf("---%s---(%d/32)", t.Format(layout), len(playersMap)))
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
