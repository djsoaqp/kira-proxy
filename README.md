# Kira-Proxy

Hi! I'm Kira, a beginner in coding, and this is my first project — a proxy-cheat for Minecraft Bedrock Edition based on [gophertunnel](https://github.com/sandertv/gophertunnel). The code might not be perfect since I'm still learning, but I hope you’ll find it fun to try! 

## Disclaimer
**This is a cheat proxy for Minecraft Bedrock Edition. It was tested on Russian-speaking servers and some others, mostly running PocketMine-MP or its forks. Using it may break server rules and get you banned. I’m not responsible for any consequences — use it at your own risk!**

## Features and Commands
Here’s what `Kira-Proxy` can do with its commands (type them in the game chat). Some features might not work perfectly yet — please let me know if you find bugs so I can fix them!

| Command            | What It Does                              | How It Works                          | Example             |
|--------------------|-------------------------------------------|---------------------------------------|---------------------|
| `.help`            | Shows all commands                       | Lists everything below in chat        | `.help`            |
| `.tp <x y z>`      | Teleports you to coordinates             | Moves you smoothly to avoid detection | `.tp 100 64 200`  |
| `.gm <0-3>`        | Changes your game mode                   | 0 = survival, 1 = creative, 2 = adventure, 3 = spectator | `.gm 1` |
| `.speed <0.1-10/off>` | Sets your speed or turns it off       | Gives you a speed boost (0.1-10)      | `.speed 5` / `.speed off` |
| `.log <on/off>`    | Turns packet logging on/off              | Logs packets to console for debugging | `.log on`         |
| `.nv <on/off>`     | Toggles night vision                     | Lets you see in the dark              | `.nv on`          |
| `.xp <on/off>`     | Auto-effects when health is low          | Adds speed, strength, regen below 5 hearts | `.xp on`    |
| `.pos`             | Shows your coordinates                   | Displays your X, Y, Z in chat         | `.pos`            |
| `.kick <msg>`      | Disconnects you with a message           | Kicks you with a custom message       | `.kick Bye`       |
| `.hide <on/off>`   | Makes you invisible or visible           | Toggles invisibility effect           | `.hide on`        |
| `.lag <sec>`       | Delays the proxy for a few seconds       | Pauses execution (for testing)        | `.lag 5`          |
| `.chat <msg>`      | Sends a message to chat                  | Speaks for you in-game                | `.chat Hello`     |
| `.radar <on/off>`  | **Not working yet!** Tracks nearby players | Should show players within 80 blocks (fix soon!) | `.radar on` |
| `.setd <n/d>`      | Sets local time to night or day          | Changes time just for you (n = night, d = day) | `.setd n` |
| `.time`            | Shows how long you’ve been playing       | Displays hours, minutes, seconds      | `.time`           |

### Notes
- **Radar**: Temporarily not supported, but I’m working on fixing it soon!
- Some features might not work properly on all servers (especially non-PocketMine ones). If something breaks, please tell me so I can improve it!

## How to Use
1. Set up the proxy (see Installation below).
2. Copy the code from `kira.go` in this repo into a new file called `kira.go` inside your `gophertunnel` folder.
3. Edit `config.toml` to connect to your server.
4. Run the proxy with `go run kira.go`.
5. Open Minecraft Bedrock and connect to `localhost:19132` (or the `LocalAddress` from `config.toml`).
6. Type commands in chat starting with `.` (like `.tp 100 64 200`).
7. Have fun, but be careful — servers might not like cheats!

## Installation

### Termux (Android)
1. **Install Termux**:
   - Get it from [F-Droid](https://f-droid.org/packages/com.termux/) (Google Play version won’t work).

2. **Install Go**:
   - Update Termux:
     ```
     pkg update && pkg upgrade
     ```
   - Install Go:
     ```
     pkg install golang
     ```
   - Check: `go version`.

3. **Download gophertunnel**:
   - Install `git`:
     ```
     pkg install git
     ```
   - Download:
     ```
     git clone https://github.com/sandertv/gophertunnel
     cd gophertunnel
     ```

4. **Add Kira-Proxy**:
   - Download `kira.go` from this repo:
     ```
     create a kira file.go : nano kira.go , then copy the code from Main.go to this repository and paste it into your file
     ```

5. **Set Up Config**:
   - Edit `config.toml` with `nano`:
     ```
     nano config.toml
     ```
   - Change `RemoteAddress` to your server (e.g., `play.nethergames.org:19132`).
   - Save (Ctrl+O, Enter, Ctrl+X).

6. **Run It**:
   - Start the proxy:
     ```
     go run kira.go
     ```

## Configuration
Edit `config.toml` in the `gophertunnel` folder to change where the proxy listens and connects:
```toml
[Connection]
LocalAddress = "0.0.0.0:19132"      # Where you connect in Minecraft
RemoteAddress = "play.nethergames.org:19132"  # The server you’re playing on
```

## Feedback
Since I’m new to coding, I’d love to hear what you think! If something doesn’t work or you have ideas, please let me know by opening an issue here on GitHub. Thanks for trying my proxy! 💜
