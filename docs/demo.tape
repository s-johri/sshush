# vhs tape for the sshush demo GIF.
#
# Generate it with:
#   vhs docs/demo.tape        # writes docs/demo.gif
#
# Install vhs: https://github.com/charmbracelet/vhs (`brew install vhs`).
# Run against a populated ~/.ssh (or an SSHUSH_SSH_DIR fixture) so the panes
# have keys and hosts to show.

Output docs/demo.gif

Set Shell bash
Set FontSize 16
Set Width 1120
Set Height 720
Set Padding 20
Set Theme "Catppuccin Mocha"

# Launch (hide the typing of the command itself).
Hide
Type "sshush"
Enter
Sleep 1s
Show

Sleep 1500ms

# Browse the Keys pane.
Down
Sleep 600ms
Down
Sleep 800ms

# Switch to the Hosts pane.
Tab
Sleep 1s
Down
Sleep 700ms

# Filter the active pane.
Type "/"
Sleep 400ms
Type "prod"
Sleep 1200ms
Escape
Sleep 700ms

# Live theme switcher.
Type "t"
Sleep 800ms
Down
Down
Sleep 700ms
Enter
Sleep 900ms

# Full keybinding help.
Type "?"
Sleep 1800ms
Escape
Sleep 600ms

# Quit.
Type "q"
Sleep 1s
