package main

import (
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"time"

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

func runJuman(s string) string {
	var out strings.Builder
	var reading strings.Builder

	cmd := exec.Command("juman")
	cmd.Stdin = strings.NewReader(s)
	cmd.Stderr = os.Stderr
	cmd.Stdout = &out

	err := cmd.Run()
	if err != nil {
		slog.Error("failed to run juman", "error", err)
		return s
	}

	lines := strings.Split(out.String(), "\n")

	for _, line := range lines {
		
		if len(line) == 0 {
			continue
		}
		
		fields := strings.Split(line, " ")
		
		if len(fields) < 2 || fields[0] == "@" {
			continue
		}
		
		reading.WriteString(fields[1])
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

func escapeString(s string) string {
	if uconvLatin {
		s = runJuman(s)
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

			m[player.PlayerUID] = player
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

			for _, player := range playersMap {
				if _, ok := prev[player.PlayerUID]; !ok {
					err := retriedBoarcast(fmt.Sprintf("player-joined:%s", player.Name))
					if err != nil {
						slog.Error("failed to broadcast", "error", err)
						continue
					}

					slog.Info("Player joined", "player", player)
				}
			}
			for _, player := range prev {
				if _, ok := playersMap[player.PlayerUID]; !ok {
					slog.Info("Player left", "player", player)

					err := retriedBoarcast(fmt.Sprintf("player-left:%s", player.Name))
					if err != nil {
						slog.Error("failed to broadcast", "error", err)
					}
				}
			}

			prev = playersMap
		}
	NEXT:
		time.Sleep(interval)
	}
}
