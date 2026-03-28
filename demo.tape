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
Type "cd /tmp && mkdir -p my-project && cd my-project"
Enter
Sleep 500ms
Show

# 1. Launch claudebar
Type "claudebar"
Sleep 500ms
Enter

# Wait for tmux + Claude to initialize
Sleep 6s

# 2. Open the menu (Alt+M) — show it off
Sleep 1s
Alt+m
Sleep 2.5s

# 3. Close menu (Escape)
Escape
Sleep 1.5s

# 4. Toggle tasks pane (Alt+T)
Alt+t
Sleep 2.5s

# 5. Toggle tasks off (Alt+T)
Alt+t
Sleep 1.5s

# 6. Toggle agents pane (Alt+A)
Alt+a
Sleep 2.5s

# 7. Toggle agents off (Alt+A)
Alt+a
Sleep 1.5s

# 8. Detach (Alt+W) — session persists in background
Alt+w
Sleep 2s
