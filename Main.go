package main

import (
    "context"
    "errors"
    "fmt"
    "github.com/pelletier/go-toml"
    "github.com/sandertv/gophertunnel/minecraft"
    "github.com/sandertv/gophertunnel/minecraft/auth"
    "github.com/sandertv/gophertunnel/minecraft/protocol/packet"
    "golang.org/x/oauth2"
    "log"
    "math"
    "os"
    "strconv"
    "strings"
    "sync"
    "time"
)

var (
    packetLoggingEnabled = false
    chatColor            = "§f"
    playerNames          = make(map[string]int64)
    playerPositions      = make(map[string][3]float32)
    radarEnabled         = false
    startTime            = time.Now()
    timeModeEnabled      = false
    forcedTime           = int32(18000) // Default: night
    xpModeEnabled        = false        // For .xp command
    lastDistances        = make(map[string]float32)
)

func main() {
    config := readConfig()
    token, err := auth.RequestLiveToken()
    if err != nil {
        log.Fatalf("\033[31m[ERROR] Failed to obtain token: %v\033[0m", err)
    }
    src := auth.RefreshTokenSource(token)

    p, err := minecraft.NewForeignStatusProvider(config.Connection.RemoteAddress)
    if err != nil {
        log.Fatalf("\033[31m[ERROR] Failed to create status provider: %v\033[0m", err)
    }
    listener, err := minecraft.ListenConfig{
        StatusProvider: p,
    }.Listen("raknet", config.Connection.LocalAddress)
    if err != nil {
        log.Fatalf("\033[31m[ERROR] Failed to start proxy: %v\033[0m", err)
    }
    defer listener.Close()
    log.Printf("\033[32m[INFO] Proxy started on %s, forwarding to %s\033[0m", config.Connection.LocalAddress, config.Connection.RemoteAddress)

    for {
        c, err := listener.Accept()
        if err != nil {
            log.Printf("\033[31m[ERROR] Failed to accept connection: %v\033[0m", err)
            continue
        }
        log.Printf("\033[33m[CONN] New connection from %s\033[0m", c.RemoteAddr())
        go handleConn(c.(*minecraft.Conn), listener, config, src)
    }
}

func handleConn(conn *minecraft.Conn, listener *minecraft.Listener, config config, src oauth2.TokenSource) {
    ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
    defer cancel()

    clientData := conn.ClientData()
    clientData.DeviceOS = 7 // Windows 10

    serverConn, err := minecraft.Dialer{
        TokenSource: src,
        ClientData:  clientData,
    }.DialContext(ctx, "raknet", config.Connection.RemoteAddress)
    if err != nil {
        log.Printf("\033[31m[ERROR] Failed to connect to server %s: %v\033[0m", config.Connection.RemoteAddress, err)
        _ = listener.Disconnect(conn, "Failed to connect to server")
        return
    }

    var g sync.WaitGroup
    g.Add(2)
    go func() {
        gameData := serverConn.GameData()
        if err := conn.StartGame(gameData); err != nil {
            log.Printf("\033[31m[ERROR] Failed to start game for %s: %v\033[0m", conn.RemoteAddr(), err)
        }
        g.Done()
    }()
    go func() {
        if err := serverConn.DoSpawn(); err != nil {
            log.Printf("\033[31m[ERROR] Failed to spawn for %s: %v\033[0m", conn.RemoteAddr(), err)
        }
        g.Done()
    }()
    g.Wait()

    packetQueue := make(chan packet.Packet, 1000)

    go func() {
        defer listener.Disconnect(conn, "Connection lost")
        defer serverConn.Close()
        for {
            pk, err := conn.ReadPacket()
            if err != nil {
                log.Printf("\033[31m[ERROR] Failed to read packet from client %s: %v\033[0m", conn.RemoteAddr(), err)
                return
            }
            if textPkt, ok := pk.(*packet.Text); ok && textPkt.TextType == packet.TextTypeChat {
                if strings.HasPrefix(textPkt.Message, ".") {
                    handleCommand(textPkt.Message, conn, listener, serverConn)
                    continue
                }
                textPkt.Message = chatColor + textPkt.Message
            }
            if pl, ok := pk.(*packet.PlayerList); ok {
                for _, entry := range pl.Entries {
                    playerNames[entry.Username] = entry.EntityUniqueID
                }
            }
            if packetLoggingEnabled {
                log.Printf("\033[36m[PACKET] Client -> Server [%s]: %+v\033[0m", time.Now().Format("15:04:05"), pk)
            }
            packetQueue <- pk
        }
    }()

    go func() {
        defer serverConn.Close()
        defer listener.Disconnect(conn, "Connection lost")
        for {
            pk, err := serverConn.ReadPacket()
            if err != nil {
                var disc minecraft.DisconnectError
                if ok := errors.As(err, &disc); ok {
                    log.Printf("\033[31m[ERROR] Server disconnected %s: %v\033[0m", conn.RemoteAddr(), disc.Error())
                    _ = listener.Disconnect(conn, disc.Error())
                } else {
                    log.Printf("\033[31m[ERROR] Failed to read packet from server for %s: %v\033[0m", conn.RemoteAddr(), err)
                }
                return
            }
            if movePkt, ok := pk.(*packet.MovePlayer); ok {
                for name, id := range playerNames {
                    if id == int64(movePkt.EntityRuntimeID) && name != conn.IdentityData().DisplayName {
                        playerPositions[name] = movePkt.Position
                        if radarEnabled {
                            myPos := conn.GameData().PlayerPosition
                            dist := float32(math.Sqrt(math.Pow(float64(myPos[0]-movePkt.Position[0]), 2) +
                                math.Pow(float64(myPos[1]-movePkt.Position[1]), 2) +
                                math.Pow(float64(myPos[2]-movePkt.Position[2]), 2)))
                            lastDist, exists := lastDistances[name]
                            if dist <= 80 {
                                if !exists || (exists && math.Abs(float64(dist-lastDist)) > 1) {
                                    if !exists {
                                        sendMessage(conn, fmt.Sprintf("Player \"%s\" detected at %.1f blocks", name, dist))
                                    } else {
                                        diff := dist - lastDist
                                        if diff > 0 {
                                            sendMessage(conn, fmt.Sprintf("Player \"%s\" moved away by %.1f blocks (distance: %.1f)", name, diff, dist))
                                        } else {
                                            sendMessage(conn, fmt.Sprintf("Player \"%s\" approached by %.1f blocks (distance: %.1f)", name, -diff, dist))
                                        }
                                    }
                                    lastDistances[name] = dist
                                }
                            } else if exists {
                                delete(lastDistances, name)
                            }
                        }
                    }
                }
            }
            if timeModeEnabled {
                if timePkt, ok := pk.(*packet.SetTime); ok {
                    timePkt.Time = forcedTime
                }
            }
            if xpModeEnabled {
                if attrPkt, ok := pk.(*packet.UpdateAttributes); ok {
                    for _, attr := range attrPkt.Attributes {
                        if attr.Name == "minecraft:health" && attr.Value < 10 { // Less than 5 hearts
                            applyXPEffects(conn)
                        }
                    }
                }
            }
            if packetLoggingEnabled {
                log.Printf("\033[36m[PACKET] Server -> Client [%s]: %+v\033[0m", time.Now().Format("15:04:05"), pk)
            }
            if err := conn.WritePacket(pk); err != nil {
                log.Printf("\033[31m[ERROR] Failed to send packet to client %s: %v\033[0m", conn.RemoteAddr(), err)
                return
            }
        }
    }()

    go func() {
        for pk := range packetQueue {
            if err := serverConn.WritePacket(pk); err != nil {
                log.Printf("\033[31m[ERROR] Failed to send packet to server for %s: %v\033[0m", conn.RemoteAddr(), err)
                return
            }
        }
    }()
}

func applyXPEffects(conn *minecraft.Conn) {
    effects := []struct {
        EffectType int32
        Amplifier  int32
    }{
        {1, 1},  // Speed
        {5, 1},  // Strength
        {10, 1}, // Regeneration
    }
    for _, effect := range effects {
        if err := conn.WritePacket(&packet.MobEffect{
            EntityRuntimeID: conn.GameData().EntityRuntimeID,
            Operation:       packet.MobEffectAdd,
            EffectType:      effect.EffectType,
            Amplifier:       effect.Amplifier,
            Particles:       false,
            Duration:        600, // 30 seconds
        }); err != nil {
            log.Printf("\033[31m[ERROR] Failed to apply effect %d for %s: %v\033[0m", effect.EffectType, conn.RemoteAddr(), err)
        }
    }
    sendMessage(conn, "Low health! Applied speed, strength, and regeneration effects.")
}

func handleCommand(message string, conn *minecraft.Conn, listener *minecraft.Listener, serverConn *minecraft.Conn) {
    parts := strings.SplitN(message, " ", 4)
    if len(parts) < 1 || !strings.HasPrefix(parts[0], ".") {
        return
    }

    command := parts[0][1:]
    switch command {
    case "help":
        helpMessage := "§l§5<Kira§r> : §7Commands:\n" +
            "§7- §c.help §f- Show command list\n" +
            "§7- §6.tp <x y z> §f- Teleport to coordinates\n" +
            "§7- §e.gm <0-3> §f- Change game mode\n" +
            "§7- §a.speed <0.1-10/off> §f- Set movement speed\n" +
            "§7- §b.log <on/off> §f- Toggle packet logging\n" +
            "§7- §9.nv <on/off> §f- Toggle night vision\n" +
            "§7- §9.xp <on/off> §f- Effects on low health\n" +
            "§7- §d.pos §f- Show current coordinates\n" +
            "§7- §5.kick <msg> §f- Disconnect with message\n" +
            "§7- §c.hide <on/off> §f- Toggle invisibility\n" +
            "§7- §6.lag <sec> §f- Delay in seconds\n" +
            "§7- §e.chat <msg> §f- Send chat message\n" +
            "§7- §a.radar <on/off> §f- Toggle player radar\n" +
            "§7- §b.setd <n/d> §f- Set local night/day\n" +
            "§7- §5.time §f- Show playtime"
        sendMessage(conn, helpMessage)

    case "chat":
        if len(parts) < 2 {
            sendError(conn, "Usage: .chat <message>")
            return
        }
        msg := strings.Join(parts[1:], " ")
        if err := conn.WritePacket(&packet.Text{
            TextType:         packet.TextTypeChat,
            NeedsTranslation: false,
            SourceName:       "",
            Message:          msg,
            XUID:             conn.IdentityData().XUID,
        }); err != nil {
            log.Printf("\033[31m[ERROR] Failed to send message for %s: %v\033[0m", conn.RemoteAddr(), err)
        }

    case "radar":
        if len(parts) < 2 || (parts[1] != "on" && parts[1] != "off") {
            sendError(conn, "Usage: .radar <on/off>")
            return
        }
        radarEnabled = parts[1] == "on"
        if !radarEnabled {
            lastDistances = make(map[string]float32)
        }
        sendMessage(conn, fmt.Sprintf("Radar: %s", parts[1]))

    case "setd":
        if len(parts) < 2 || (parts[1] != "night" && parts[1] != "day") {
            sendError(conn, "Usage: .setd <night/day>")
            return
        }
        timeModeEnabled = true
        if parts[1] == "night" {
            forcedTime = 18000
        } else {
            forcedTime = 6000
        }
        if err := conn.WritePacket(&packet.SetTime{
            Time: forcedTime,
        }); err != nil {
            log.Printf("\033[31m[ERROR] Failed to set time for %s: %v\033[0m", conn.RemoteAddr(), err)
            return
        }
        sendMessage(conn, fmt.Sprintf("Time set to: %s", parts[1]))

    case "time":
        duration := time.Since(startTime)
        hours := int(duration.Hours())
        minutes := int(duration.Minutes()) % 60
        seconds := int(duration.Seconds()) % 60
        sendMessage(conn, fmt.Sprintf("Playtime: %dh %dm %ds", hours, minutes, seconds))

    case "tp":
        if len(parts) != 4 {
            sendError(conn, "Usage: .tp <x> <y> <z>")
            return
        }
        x, err1 := strconv.ParseFloat(parts[1], 32)
        y, err2 := strconv.ParseFloat(parts[2], 32)
        z, err3 := strconv.ParseFloat(parts[3], 32)
        if err1 != nil || err2 != nil || err3 != nil {
            sendError(conn, "Coordinates must be numbers")
            return
        }
        target := [3]float32{float32(x), float32(y), float32(z)}
        currentPos := conn.GameData().PlayerPosition
        distance := float32(math.Sqrt(math.Pow(float64(target[0]-currentPos[0]), 2) +
            math.Pow(float64(target[1]-currentPos[1]), 2) +
            math.Pow(float64(target[2]-currentPos[2]), 2)))
        steps := int(distance / 0.5) // Max 0.5 blocks per tick
        if steps < 1 {
            steps = 1
        }
        dx := (target[0] - currentPos[0]) / float32(steps)
        dy := (target[1] - currentPos[1]) / float32(steps)
        dz := (target[2] - currentPos[2]) / float32(steps)
        for i := 0; i < steps; i++ {
            currentPos[0] += dx
            currentPos[1] += dy
            currentPos[2] += dz
            if err := conn.WritePacket(&packet.MovePlayer{
                EntityRuntimeID: conn.GameData().EntityRuntimeID,
                Position:        currentPos,
                Pitch:           0,
                Yaw:             0,
                HeadYaw:         0,
                Mode:            packet.MoveModeNormal,
                OnGround:        true,
            }); err != nil {
                log.Printf("\033[31m[ERROR] Teleportation failed for %s: %v\033[0m", conn.RemoteAddr(), err)
                return
            }
            time.Sleep(20 * time.Millisecond) // 1 tick = 50ms, but slightly faster
        }
        // Final position
        if err := conn.WritePacket(&packet.MovePlayer{
            EntityRuntimeID: conn.GameData().EntityRuntimeID,
            Position:        target,
            Pitch:           0,
            Yaw:             0,
            HeadYaw:         0,
            Mode:            packet.MoveModeNormal,
            OnGround:        true,
        }); err != nil {
            log.Printf("\033[31m[ERROR] Teleportation failed for %s: %v\033[0m", conn.RemoteAddr(), err)
            return
        }
        sendMessage(conn, fmt.Sprintf("Teleported to %.1f %.1f %.1f", x, y, z))

    case "gm":
        if len(parts) < 2 {
            sendError(conn, "Usage: .gm <0|1|2|3>")
            return
        }
        mode, err := strconv.Atoi(parts[1])
        if err != nil || mode < 0 || mode > 3 {
            sendError(conn, "Mode must be 0, 1, 2, or 3")
            return
        }
        if err := conn.WritePacket(&packet.SetPlayerGameType{
            GameType: int32(mode),
        }); err != nil {
            log.Printf("\033[31m[ERROR] Failed to change game mode for %s: %v\033[0m", conn.RemoteAddr(), err)
            return
        }
        modeNames := map[int]string{0: "survival", 1: "creative", 2: "adventure", 3: "spectator"}
        sendMessage(conn, fmt.Sprintf("Game mode set to: %s", modeNames[mode]))

    case "speed":
        if len(parts) < 2 {
            sendError(conn, "Usage: .speed <0.1-10/off>")
            return
        }
        if parts[1] == "off" {
            if err := conn.WritePacket(&packet.MobEffect{
                EntityRuntimeID: conn.GameData().EntityRuntimeID,
                Operation:       packet.MobEffectRemove,
                EffectType:      1,
            }); err != nil {
                log.Printf("\033[31m[ERROR] Failed to remove speed for %s: %v\033[0m", conn.RemoteAddr(), err)
                return
            }
            sendMessage(conn, "Speed disabled")
            return
        }
        speed, err := strconv.ParseFloat(parts[1], 32)
        if err != nil || speed < 0.1 || speed > 10 {
            sendError(conn, "Speed must be 0.1-10 or 'off'")
            return
        }
        if err := conn.WritePacket(&packet.MobEffect{
            EntityRuntimeID: conn.GameData().EntityRuntimeID,
            Operation:       packet.MobEffectAdd,
            EffectType:      1,
            Amplifier:       int32(speed-1),
            Particles:       false,
            Duration:        999999,
        }); err != nil {
            log.Printf("\033[31m[ERROR] Failed to set speed for %s: %v\033[0m", conn.RemoteAddr(), err)
            return
        }
        sendMessage(conn, fmt.Sprintf("Speed set to: %.1f", speed))

    case "log":
        if len(parts) < 2 || (parts[1] != "on" && parts[1] != "off") {
            sendError(conn, "Usage: .log <on/off>")
            return
        }
        packetLoggingEnabled = parts[1] == "on"
        sendMessage(conn, fmt.Sprintf("Packet logging: %s", parts[1]))

    case "nv":
        if len(parts) < 2 || (parts[1] != "on" && parts[1] != "off") {
            sendError(conn, "Usage: .nv <on/off>")
            return
        }
        if parts[1] == "on" {
            if err := conn.WritePacket(&packet.MobEffect{
                EntityRuntimeID: conn.GameData().EntityRuntimeID,
                Operation:       packet.MobEffectAdd,
                EffectType:      16,
                Amplifier:       1,
                Particles:       false,
                Duration:        999999,
            }); err != nil {
                log.Printf("\033[31m[ERROR] Failed to enable night vision for %s: %v\033[0m", conn.RemoteAddr(), err)
                return
            }
            sendMessage(conn, "Night vision: on")
        } else {
            if err := conn.WritePacket(&packet.MobEffect{
                EntityRuntimeID: conn.GameData().EntityRuntimeID,
                Operation:       packet.MobEffectRemove,
                EffectType:      16,
            }); err != nil {
                log.Printf("\033[31m[ERROR] Failed to disable night vision for %s: %v\033[0m", conn.RemoteAddr(), err)
                return
            }
            sendMessage(conn, "Night vision: off")
        }

    case "xp":
        if len(parts) < 2 || (parts[1] != "on" && parts[1] != "off") {
            sendError(conn, "Usage: .xp <on/off>")
            return
        }
        xpModeEnabled = parts[1] == "on"
        sendMessage(conn, fmt.Sprintf("XP mode: %s", parts[1]))

    case "pos":
        pos := conn.GameData().PlayerPosition
        sendMessage(conn, fmt.Sprintf("Coordinates: %.1f %.1f %.1f", pos[0], pos[1], pos[2]))

    case "kick":
        if len(parts) < 2 {
            sendError(conn, "Usage: .kick <message>")
            return
        }
        msg := strings.Join(parts[1:], " ")
        listener.Disconnect(conn, msg)
        log.Printf("\033[33m[CONN] Player %s disconnected: %s\033[0m", conn.RemoteAddr(), msg)

    case "hide":
        if len(parts) < 2 || (parts[1] != "on" && parts[1] != "off") {
            sendError(conn, "Usage: .hide <on/off>")
            return
        }
        hide := parts[1] == "on"
        duration := int32(999999)
        if !hide {
            duration = 0
        }
        if err := conn.WritePacket(&packet.MobEffect{
            EntityRuntimeID: conn.GameData().EntityRuntimeID,
            Operation:       packet.MobEffectAdd,
            EffectType:      14,
            Amplifier:       1,
            Particles:       false,
            Duration:        duration,
        }); err != nil {
            log.Printf("\033[31m[ERROR] Failed to toggle visibility for %s: %v\033[0m", conn.RemoteAddr(), err)
            return
        }
        status := "invisible"
        if !hide {
            status = "visible"
        }
        sendMessage(conn, fmt.Sprintf("You are now %s", status))

    case "lag":
        if len(parts) < 2 {
            sendError(conn, "Usage: .lag <seconds>")
            return
        }
        duration, err := strconv.Atoi(parts[1])
        if err != nil || duration < 1 {
            sendError(conn, "Specify a number > 0")
            return
        }
        time.Sleep(time.Duration(duration) * time.Second)
        sendMessage(conn, fmt.Sprintf("Delay: %d sec", duration))

    default:
        sendError(conn, "Unknown command. Use .help")
    }
}

func sendMessage(conn *minecraft.Conn, msg string) {
    if err := conn.WritePacket(&packet.Text{
        TextType:         packet.TextTypeChat,
        NeedsTranslation: false,
        SourceName:       "",
        Message:          "<§5Kira§r> : " + msg,
        XUID:             conn.IdentityData().XUID,
    }); err != nil {
        log.Printf("\033[31m[ERROR] Failed to send message to %s: %v\033[0m", conn.RemoteAddr(), err)
    }
}

func sendError(conn *minecraft.Conn, msg string) {
    if err := conn.WritePacket(&packet.Text{
        TextType:         packet.TextTypeChat,
        NeedsTranslation: false,
        SourceName:       "",
        Message:          "<§5Kira§r> : §c" + msg,
        XUID:             conn.IdentityData().XUID,
    }); err != nil {
        log.Printf("\033[31m[ERROR] Failed to send error to %s: %v\033[0m", conn.RemoteAddr(), err)
    }
}

type config struct {
    Connection struct {
        LocalAddress  string
        RemoteAddress string
    }
}

func readConfig() config {
    c := config{}
    if _, err := os.Stat("config.toml"); os.IsNotExist(err) {
        f, err := os.Create("config.toml")
        if err != nil {
            log.Fatalf("\033[31m[ERROR] Failed to create config.toml: %v\033[0m", err)
        }
        data, err := toml.Marshal(c)
        if err != nil {
            log.Fatalf("\033[31m[ERROR] Failed to encode config: %v\033[0m", err)
        }
        if _, err := f.Write(data); err != nil {
            log.Fatalf("\033[31m[ERROR] Failed to write config: %v\033[0m", err)
        }
        _ = f.Close()
    }
    data, err := os.ReadFile("config.toml")
    if err != nil {
        log.Fatalf("\033[31m[ERROR] Failed to read config.toml: %v\033[0m", err)
    }
    if err := toml.Unmarshal(data, &c); err != nil {
        log.Fatalf("\033[31m[ERROR] Failed to decode config: %v\033[0m", err)
    }
    if c.Connection.LocalAddress == "" {
        c.Connection.LocalAddress = "0.0.0.0:19132"
    }
    if c.Connection.RemoteAddress == "" {
        c.Connection.RemoteAddress = "play.nethergames.org:19132"
    }
    data, _ = toml.Marshal(c)
    if err := os.WriteFile("config.toml", data, 0644); err != nil {
        log.Fatalf("\033[31m[ERROR] Failed to write config: %v\033[0m", err)
    }
    return c
}
