# truffles help — short banner + examples + version.
#
#   make demo-vhs TAPE=help
#   vhs assets/help.tape

Output assets/screenshots/help.gif
Output assets/screenshots/help.mp4

Require go
Require ffmpeg

Set Shell "bash"
Set FontSize 15
Set Width 1280
Set Height 800
Set TypingSpeed 42ms
Set Theme "Catppuccin Mocha"
Set Padding 16
Set BorderRadius 8
Set WindowBar Colorful
Set Framerate 30

Hide
Type "unset NO_COLOR FORCE_COLOR; export TERM=xterm-256color CLICOLOR=1"
Enter
Sleep 300ms
Type "test -x ./truffles || go build -o truffles ./cmd/truffles"
Enter
Sleep 2s
Type "clear"
Enter
Sleep 500ms
Show

Type "./truffles"
Sleep 1.5s
Enter
Sleep 5s

Type "./truffles examples"
Sleep 1.5s
Enter
Sleep 6s

Type "./truffles version"
Sleep 1.2s
Enter
Sleep 3.5s
