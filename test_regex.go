package main

import (
	"fmt"
	"regexp"
)

func UpdateTargetHost(target, destHost string) string {
	if destHost == "" {
		return target
	}
	pattern := regexp.MustCompile(`^([^@]+@)([^:]+)(::.*)$`)
	if pattern.MatchString(target) {
		return pattern.ReplaceAllString(target, "${1}"+destHost+"${3}")
	}
	return target
}

func main() {
	fmt.Println(UpdateTargetHost("syncuser@receiver::video-sync/Doku", "schnorarr-receiver.werewolf-gondola.ts.net"))
	fmt.Println(UpdateTargetHost("syncuser@receiver::video-sync/Serien", "schnorarr-receiver.werewolf-gondola.ts.net"))
	fmt.Println(UpdateTargetHost("syncuser@receiver::video-sync/Anime", "schnorarr-receiver.werewolf-gondola.ts.net"))
}
