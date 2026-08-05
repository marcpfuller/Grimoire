package main

import "github.com/charmbracelet/huh"

// grimTheme returns the huh theme used by all interactive prompts.
//
// huh.ThemeBase() styles selection/emphasis using only the basic ANSI color
// codes ("0"-"15"), which terminal emulators let users remap to their own
// palette. Unlike huh.ThemeCharm() (the library default), it has no
// hardcoded hex/256-color values, so prompts render using whatever colors
// the user's terminal theme defines instead of a fixed brand palette.
func grimTheme() *huh.Theme {
	return huh.ThemeBase()
}
