# claudebar demo — showcasing key features
# Run with: vhs demo.tape

Output demo.gif

Require claudebar

Set Shell "zsh"
Set Width 120
Set Height 35
Set FontSize 16
Set Padding 12
Set Theme "Catppuccin Mocha"
Set TypingSpeed 80ms
Set Framerate 30

# Start in a sample project directory
Hide
Type "cd /tmp && mkdir -p demo-project && cd demo-project"
Enter
Sleep 500ms
Show

# 1. Launch claudebar
Type "claudebar"
Sleep 500ms
Enter

# Wait for tmux + Claude to initialize
Sleep 8s

# 2. Open the menu
Alt+M
Sleep 3s

# 3. Close menu
Escape
Sleep 1.5s

# 4. Toggle tasks pane
Alt+T
Sleep 3s

# 5. Toggle tasks off
Alt+T
Sleep 1.5s

# 6. Toggle agents pane
Alt+A
Sleep 3s

# 7. Toggle agents off
Alt+A
Sleep 1.5s

# 8. Detach — session persists in background
Alt+W
Sleep 2s
