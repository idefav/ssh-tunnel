package views

import "embed"

//go:embed *.gohtml
var HtmlFs embed.FS

//go:embed resources/*
var StaticFs embed.FS
