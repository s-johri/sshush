# vhs tape for the app screenshot used in the social preview card.
#
# Generate the finished 1280x640 card with:
#   sh docs/demo-fixture.sh docs/social.tape   # writes docs/social/app.png
#   python3 docs/social-card.py                # composes docs/social-preview.png
#
# Upload the card at github.com/s-johri/sshush/settings (Social preview).

Output docs/social/frames.gif

Set Shell bash
Set FontSize 16
Set Width 1280
Set Height 400
Set Padding 20
Set Theme "TokyoNight"

Hide
Type "sshush"
Enter
Sleep 2s
Show
Down
Sleep 500ms
Screenshot docs/social/app.png
Sleep 500ms
Hide
Type "q"
Sleep 1s
