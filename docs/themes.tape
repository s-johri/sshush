# vhs tape for the theme screenshots embedded in the README.
#
# Generate them with:
#   sh docs/demo-fixture.sh docs/themes.tape   # writes docs/themes/*.png
#
# Each block rewrites the fixture's config.toml theme and relaunches sshush,
# so every screenshot is the same Keys-pane view under a different theme.

Output docs/themes/frames.gif

Set Shell bash
Set FontSize 16
Set Width 1120
Set Height 400
Set Padding 20
Set Theme "TokyoNight"

Hide
Type `sed -i 's/^theme.*/theme = "dracula"/' "$XDG_CONFIG_HOME/sshush/config.toml" && sshush`
Enter
Sleep 2s
Show
Sleep 500ms
Screenshot docs/themes/dracula.png
Sleep 500ms
Hide
Type "q"
Sleep 1.5s

Type `sed -i 's/^theme.*/theme = "nord"/' "$XDG_CONFIG_HOME/sshush/config.toml" && sshush`
Enter
Sleep 2s
Show
Sleep 500ms
Screenshot docs/themes/nord.png
Sleep 500ms
Hide
Type "q"
Sleep 1.5s

Type `sed -i 's/^theme.*/theme = "catppuccin-mocha"/' "$XDG_CONFIG_HOME/sshush/config.toml" && sshush`
Enter
Sleep 2s
Show
Sleep 500ms
Screenshot docs/themes/catppuccin-mocha.png
Sleep 500ms
Hide
Type "q"
Sleep 1.5s

Type `sed -i 's/^theme.*/theme = "solarized-light"/' "$XDG_CONFIG_HOME/sshush/config.toml" && sshush`
Enter
Sleep 2s
Show
Sleep 500ms
Screenshot docs/themes/solarized-light.png
Sleep 500ms
Hide
Type "q"
Sleep 1.5s
