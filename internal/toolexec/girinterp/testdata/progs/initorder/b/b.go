package b

import "example.com/initorder/c"

var B = len(c.C) * 10

func init() { println("b init", B) }
