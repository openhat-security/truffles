# truffles — full owner enumeration of openhat-security.
#
#   make demo-vhs TAPE=search
#   vhs assets/search.tape

Output assets/screenshots/search.gif
Output assets/screenshots/search.mp4

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
Type "rm -f repos-ohs.txt"
Enter
Sleep 400ms
Type "clear"
Enter
Sleep 500ms
Show

Type "./truffles"
Sleep 1.5s
Enter
Sleep 4.5s

Type "./truffles search -no-proxy -owner openhat-security -out repos-ohs.txt"
Sleep 2s
Enter
Sleep 8s

Type "wc -l repos-ohs.txt && head -10 repos-ohs.txt"
Sleep 1.5s
Enter
Sleep 5s

Type "echo '✓ openhat-security org search complete'"
Sleep 1.2s
Enter
Sleep 3.5s
