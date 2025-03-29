package util

import "net"

func IsIPAddress(input string) bool {
	ip := net.ParseIP(input)
	return ip != nil
}

func IsIPv4(input string) bool {
	ip := net.ParseIP(input)
	return ip != nil && ip.To4() != nil
}
