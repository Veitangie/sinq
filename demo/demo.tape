# Renders the sinq showcase into docs/assets/demo.gif, embedded in both
# README.md and docs/index.md.
#
# Run from the repo root:
#   vhs demo/demo.tape
#
# Requires the vhs CLI: https://github.com/charmbracelet/vhs

Output docs/assets/demo.gif

Require go

Set Shell "zsh"
Set FontSize 20
Set FontFamily "FiraCode Nerd Font Mono"
Set Width 1200
Set Height 680
Set Padding 20
Set Theme {"name":"macOS Terminal Vivid","background":"#1e1e1e","foreground":"#ffffff","black":"#000000","red":"#e50000","green":"#00d900","yellow":"#e5e500","blue":"#0000ff","magenta":"#e500e5","cyan":"#00e5e5","white":"#e5e5e5","brightBlack":"#666666","brightRed":"#ff6e67","brightGreen":"#5ffa68","brightYellow":"#fffc67","brightBlue":"#6871ff","brightMagenta":"#ff77ff","brightCyan":"#60fdff","brightWhite":"#ffffff","cursor":"#bfbfbf","selection":"#4d4d4d"}
Set WindowBar Colorful
Set BorderRadius 20
Set TypingSpeed 55ms

Hide
Type "source demo/record-setup.sh"
Enter
Wait+Screen@30s /demo server listening/
Sleep 300ms
Type "clear; printf '\n%.0s' {1..40}"
Enter
Sleep 500ms
Show

Type "sinq -pw15 demo"
Sleep 600ms
Enter

Sleep 10s
