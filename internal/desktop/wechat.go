package desktop

// WeChat has two independent installation routes. Keeping the mapping pure
// makes the four-way desktop dialog testable without starting an installer.
const (
	wechatRouteWine  = "wine"
	wechatRouteArch  = "arch"
	wechatRouteBoth  = "both"
	wechatRouteNone  = ""
	wechatRouteCount = 4
)

func wechatRouteForChoice(choice int) (string, bool) {
	switch choice {
	case 0:
		return wechatRouteWine, true
	case 1:
		return wechatRouteArch, true
	case 2:
		return wechatRouteBoth, true
	default:
		return wechatRouteNone, false
	}
}

func wechatRoutes(route string) []string {
	switch route {
	case wechatRouteWine:
		return []string{wechatRouteWine}
	case wechatRouteArch:
		return []string{wechatRouteArch}
	case wechatRouteBoth:
		return []string{wechatRouteWine, wechatRouteArch}
	default:
		return nil
	}
}
