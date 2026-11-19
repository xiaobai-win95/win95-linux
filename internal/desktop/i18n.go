package desktop

import (
	"fmt"
	"os"
	"strings"
	"time"
)

type languageDef struct {
	Code       string
	Name       string
	English    string
	DateLayout string
	Clock12    bool
}

// The languages below ship with a UI translation for the desktop shell and
// the settings app. Scripts without a translation fall back to English.
var desktopLanguages = []languageDef{
	{"zh-CN", "简体中文", "Chinese (Simplified)", "2006-01-02", false},
	{"zh-TW", "繁體中文", "Chinese (Traditional)", "2006-01-02", false},
	{"en-US", "English", "English (United States)", "01/02/2006", true},
	{"ja-JP", "日本語", "Japanese", "2006/01/02", false},
	{"ko-KR", "한국어", "Korean", "2006-01-02", false},
	{"fr-FR", "Français", "French", "02/01/2006", false},
	{"de-DE", "Deutsch", "German", "02.01.2006", false},
	{"es-ES", "Español", "Spanish", "02/01/2006", false},
	{"ru-RU", "Русский", "Russian", "02.01.2006", false},
	{"pt-BR", "Português", "Portuguese (Brazil)", "02/01/2006", false},
	{"it-IT", "Italiano", "Italian", "02/01/2006", false},
}

func languageIndex(code string) int {
	for i, l := range desktopLanguages {
		if l.Code == code {
			return i
		}
	}
	return 0
}

// tr translates a Chinese source string into the current UI language.
func (d *Desktop) tr(s string) string {
	if d.lang == "" || d.lang == "zh-CN" {
		return s
	}
	if tbl, ok := uiTranslations[d.lang]; ok {
		if v, ok := tbl[s]; ok {
			return v
		}
	}
	if tbl, ok := extraTranslations[d.lang]; ok {
		if v, ok := tbl[s]; ok {
			return v
		}
	}
	if d.lang != "en-US" {
		if v, ok := uiTranslations["en-US"][s]; ok {
			return v
		}
		if v, ok := extraTranslations["en-US"][s]; ok {
			return v
		}
	}
	return s
}

// extraTranslations holds strings added after the main table was written.
var extraTranslations = map[string]map[string]string{
	"zh-TW": {
		"三维弹球": "3D 彈珠台", "重新开始": "重新開始", "发射": "發射", "分数": "分數",
		"剩余球": "剩餘球", "最高分": "最高分", "游戏结束": "遊戲結束",
	},
	"en-US": {
		"三维弹球": "3D Pinball", "重新开始": "New Game", "发射": "Launch", "分数": "Score",
		"剩余球": "Balls", "最高分": "High Score", "游戏结束": "Game Over",
	},
	"ja-JP": {
		"三维弹球": "3D ピンボール", "重新开始": "新しいゲーム", "发射": "発射", "分数": "スコア",
		"剩余球": "残り球", "最高分": "ハイスコア", "游戏结束": "ゲームオーバー",
	},
	"ko-KR": {
		"三维弹球": "3D 핀볼", "重新开始": "새 게임", "发射": "발사", "分数": "점수",
		"剩余球": "남은 공", "最高分": "최고 점수", "游戏结束": "게임 오버",
	},
	"fr-FR": {
		"三维弹球": "Pinball 3D", "重新开始": "Nouvelle partie", "发射": "Lancer", "分数": "Score",
		"剩余球": "Balles", "最高分": "Record", "游戏结束": "Partie terminée",
	},
	"de-DE": {
		"三维弹球": "3D-Flipper", "重新开始": "Neues Spiel", "发射": "Abschuss", "分数": "Punkte",
		"剩余球": "Kugeln", "最高分": "Bestwert", "游戏结束": "Spiel vorbei",
	},
	"es-ES": {
		"三维弹球": "Pinball 3D", "重新开始": "Nueva partida", "发射": "Lanzar", "分数": "Puntos",
		"剩余球": "Bolas", "最高分": "Récord", "游戏结束": "Fin de la partida",
	},
	"ru-RU": {
		"三维弹球": "Пинбол 3D", "重新开始": "Новая игра", "发射": "Запуск", "分数": "Счёт",
		"剩余球": "Шары", "最高分": "Рекорд", "游戏结束": "Игра окончена",
	},
	"pt-BR": {
		"三维弹球": "Pinball 3D", "重新开始": "Novo jogo", "发射": "Lançar", "分数": "Pontos",
		"剩余球": "Bolas", "最高分": "Recorde", "游戏结束": "Fim de jogo",
	},
	"it-IT": {
		"三维弹球": "Pinball 3D", "重新开始": "Nuova partita", "发射": "Lancio", "分数": "Punti",
		"剩余球": "Palline", "最高分": "Record", "游戏结束": "Partita finita",
	},
}

// trf translates a format string and formats its arguments.
func (d *Desktop) trf(s string, args ...any) string {
	return fmt.Sprintf(d.tr(s), args...)
}

var uiTranslations = map[string]map[string]string{
	"zh-TW": {
		"开始": "開始", "程序(P)": "程式(P)", "文档(D)": "文件(D)", "设置(S)": "設定(S)",
		"查找(F)": "尋找(F)", "帮助(H)": "說明(H)", "运行(R)…": "執行(R)…",
		"注销(L)…": "登出(L)…", "重新启动(R)…": "重新啟動(R)…", "关闭计算机(U)…": "關閉電腦(U)…",
		"睡眠(S)…": "睡眠(S)…", "我的电脑": "我的電腦", "我的文档": "我的文件",
		"网络浏览器": "網路瀏覽器", "方块世界 95": "方塊世界 95", "XFCE 终端": "XFCE 終端機", "注册表编辑器": "登錄編輯程式",
		"XFCE 桌面": "XFCE 桌面", "回收站": "資源回收筒", "记事本": "記事本",
		"计算器": "計算機", "日历": "行事曆", "终端": "終端機", "文件管理器": "檔案總管",
		"纸牌": "接龍", "扫雷": "採地雷", "画图": "小畫家", "图片查看器": "圖片檢視器",
		"图形化下载器": "下載管理員", "软件包管理器": "套件管理員", "屏幕键盘": "螢幕小鍵盤",
		"系统信息": "系統資訊", "设置中心": "設定中心", "关于本机": "關於本機",
		"显示属性(D)": "顯示內容(D)", "主题": "佈景主題", "语言": "語言", "字体": "字型",
		"时间": "時間", "地区": "地區", "关于": "關於", "许可协议": "授權合約",
		"确定": "確定", "取消": "取消", "应用": "套用", "关闭": "關閉", "打开": "開啟",
		"新游戏": "新遊戲", "开始游戏": "開始遊戲", "选择界面语言，应用后立即生效。": "選擇介面語言，套用後立即生效。",
		"选择主题，应用后立即生效。": "選擇佈景主題，套用後立即生效。",
		"时区": "時區", "日期格式": "日期格式", "时间格式": "時間格式", "24 小时制": "24 小時制",
		"一周开始": "一週開始", "星期一": "星期一", "星期日": "星期日", "当前时间": "目前時間",
		"字体预览": "字型預覽", "字体覆盖": "字型涵蓋範圍", "世界语言与字体已全部安装。": "世界語言與字型已全部安裝。",
		"版本": "版本", "系统": "系統", "许可证": "授權條款", "本系统包含的第三方组件：": "本系統包含的第三方元件：",
		"界面语言": "介面語言", "桌面与窗口颜色": "桌面與視窗顏色", "显示": "顯示",
		"打开显示属性…": "開啟顯示內容…", "语言与格式": "語言與格式", "区域预设": "區域預設",
		"应用区域设置": "套用區域設定", "此区域将设置语言、时区和日期格式。": "此區域將設定語言、時區和日期格式。",
	},
	"en-US": {
		"开始": "Start", "程序(P)": "Programs", "文档(D)": "Documents", "设置(S)": "Settings",
		"查找(F)": "Find", "帮助(H)": "Help", "运行(R)…": "Run…",
		"注销(L)…": "Log Off…", "重新启动(R)…": "Restart…", "关闭计算机(U)…": "Shut Down…",
		"睡眠(S)…": "Sleep…", "我的电脑": "My Computer", "我的文档": "My Documents",
		"网络浏览器": "Web Browser", "方块世界 95": "Block World 95", "XFCE 终端": "XFCE Terminal", "注册表编辑器": "Registry Editor",
		"XFCE 桌面": "XFCE Desktop", "回收站": "Recycle Bin", "记事本": "Notepad",
		"计算器": "Calculator", "日历": "Calendar", "终端": "Terminal", "文件管理器": "File Manager",
		"纸牌": "Solitaire", "扫雷": "Minesweeper", "画图": "Paint", "图片查看器": "Image Viewer",
		"图形化下载器": "Download Manager", "软件包管理器": "Package Manager",
		"屏幕键盘": "On-Screen Keyboard", "系统信息": "System Information", "设置中心": "Settings",
		"关于本机": "About This Computer", "显示属性(D)": "Display Properties",
		"主题": "Theme", "语言": "Language", "字体": "Fonts", "时间": "Time", "地区": "Region",
		"关于": "About", "许可协议": "License", "确定": "OK", "取消": "Cancel",
		"应用": "Apply", "关闭": "Close", "打开": "Open", "新游戏": "New Game",
		"开始游戏": "Start Game", "选择界面语言，应用后立即生效。": "Choose a display language. Changes apply immediately.",
		"选择主题，应用后立即生效。": "Choose a theme. Changes apply immediately.",
		"时区": "Time zone", "日期格式": "Date format", "时间格式": "Time format",
		"24 小时制": "24-hour clock", "一周开始": "First day of week", "星期一": "Monday",
		"星期日": "Sunday", "当前时间": "Current time", "字体预览": "Font preview",
		"字体覆盖": "Font coverage", "世界语言与字体已全部安装。": "All world language fonts are installed.",
		"版本": "Version", "系统": "System", "许可证": "License",
		"本系统包含的第三方组件：": "Third-party components included:",
		"界面语言":         "Display language", "桌面与窗口颜色": "Desktop and window colours",
		"显示": "Display", "打开显示属性…": "Open Display Properties…",
		"语言与格式": "Language and formats", "区域预设": "Region presets",
		"应用区域设置": "Apply region settings",
		"此区域将设置语言、时区和日期格式。": "This region sets the language, time zone and date format.",
	},
	"ja-JP": {
		"开始": "スタート", "程序(P)": "プログラム", "文档(D)": "ドキュメント", "设置(S)": "設定",
		"查找(F)": "検索", "帮助(H)": "ヘルプ", "运行(R)…": "ファイル名を指定して実行…",
		"注销(L)…": "ログオフ…", "重新启动(R)…": "再起動…", "关闭计算机(U)…": "シャットダウン…",
		"睡眠(S)…": "スリープ…", "我的电脑": "マイ コンピュータ", "我的文档": "マイ ドキュメント",
		"网络浏览器": "ウェブ ブラウザ", "方块世界 95": "ブロックワールド 95",
		"XFCE 终端": "XFCE 端末", "XFCE 桌面": "XFCE デスクトップ", "回收站": "ごみ箱", "注册表编辑器": "レジストリ エディター",
		"记事本": "メモ帳", "计算器": "電卓", "日历": "カレンダー", "终端": "端末",
		"文件管理器": "エクスプローラー", "纸牌": "ソリティア", "扫雷": "マインスイーパ",
		"画图": "ペイント", "图片查看器": "画像ビューア", "图形化下载器": "ダウンローダー",
		"软件包管理器": "パッケージマネージャ", "屏幕键盘": "スクリーン キーボード",
		"系统信息": "システム情報", "设置中心": "設定", "关于本机": "バージョン情報",
		"显示属性(D)": "画面のプロパティ", "主题": "テーマ", "语言": "言語", "字体": "フォント",
		"时间": "時刻", "地区": "地域", "关于": "情報", "许可协议": "ライセンス",
		"确定": "OK", "取消": "キャンセル", "应用": "適用", "关闭": "閉じる", "打开": "開く",
		"新游戏": "新しいゲーム", "开始游戏": "ゲーム開始",
		"选择界面语言，应用后立即生效。": "表示言語を選択してください。すぐに反映されます。",
		"选择主题，应用后立即生效。":   "テーマを選択してください。すぐに反映されます。",
		"时区": "タイムゾーン", "日期格式": "日付形式", "时间格式": "時刻形式",
		"24 小时制": "24 時間制", "一周开始": "週の開始日", "星期一": "月曜日",
		"星期日": "日曜日", "当前时间": "現在時刻", "字体预览": "フォントのプレビュー",
		"字体覆盖": "フォントの対応範囲", "世界语言与字体已全部安装。": "世界の言語フォントはすべてインストール済みです。",
		"版本": "バージョン", "系统": "システム", "许可证": "ライセンス",
		"本系统包含的第三方组件：": "同梱のサードパーティ製コンポーネント：",
		"界面语言":         "表示言語", "桌面与窗口颜色": "デスクトップとウィンドウの色",
		"显示": "画面", "打开显示属性…": "画面のプロパティを開く…",
		"语言与格式": "言語と形式", "区域预设": "地域プリセット",
		"应用区域设置": "地域設定を適用",
		"此区域将设置语言、时区和日期格式。": "この地域では言語、タイムゾーン、日付形式を設定します。",
	},
	"ko-KR": {
		"开始": "시작", "程序(P)": "프로그램", "文档(D)": "문서", "设置(S)": "설정",
		"查找(F)": "찾기", "帮助(H)": "도움말", "运行(R)…": "실행…",
		"注销(L)…": "로그오프…", "重新启动(R)…": "다시 시작…", "关闭计算机(U)…": "시스템 종료…",
		"睡眠(S)…": "절전…", "我的电脑": "내 컴퓨터", "我的文档": "내 문서",
		"网络浏览器": "웹 브라우저", "方块世界 95": "블록 월드 95",
		"XFCE 终端": "XFCE 터미널", "XFCE 桌面": "XFCE 데스크톱", "回收站": "휴지통", "注册表编辑器": "레지스트리 편집기",
		"记事本": "메모장", "计算器": "계산기", "日历": "달력", "终端": "터미널",
		"文件管理器": "파일 관리자", "纸牌": "카드 놀이", "扫雷": "지뢰 찾기",
		"画图": "그림판", "图片查看器": "이미지 뷰어", "图形化下载器": "다운로드 관리자",
		"软件包管理器": "패키지 관리자", "屏幕键盘": "화상 키보드",
		"系统信息": "시스템 정보", "设置中心": "설정", "关于本机": "정보",
		"显示属性(D)": "디스플레이 등록 정보", "主题": "테마", "语言": "언어", "字体": "글꼴",
		"时间": "시간", "地区": "지역", "关于": "정보", "许可协议": "라이선스",
		"确定": "확인", "取消": "취소", "应用": "적용", "关闭": "닫기", "打开": "열기",
		"新游戏": "새 게임", "开始游戏": "게임 시작",
		"选择界面语言，应用后立即生效。": "표시 언어를 선택하세요. 즉시 적용됩니다.",
		"选择主题，应用后立即生效。":   "테마를 선택하세요. 즉시 적용됩니다.",
		"时区": "시간대", "日期格式": "날짜 형식", "时间格式": "시간 형식",
		"24 小时制": "24시간제", "一周开始": "한 주의 시작", "星期一": "월요일",
		"星期日": "일요일", "当前时间": "현재 시간", "字体预览": "글꼴 미리 보기",
		"字体覆盖": "글꼴 지원 범위", "世界语言与字体已全部安装。": "모든 세계 언어 글꼴이 설치되어 있습니다.",
		"版本": "버전", "系统": "시스템", "许可证": "라이선스",
		"本系统包含的第三方组件：": "포함된 타사 구성 요소:",
		"界面语言":         "표시 언어", "桌面与窗口颜色": "데스크톱 및 창 색상",
		"显示": "디스플레이", "打开显示属性…": "디스플레이 등록 정보 열기…",
		"语言与格式": "언어 및 형식", "区域预设": "지역 프리셋",
		"应用区域设置": "지역 설정 적용",
		"此区域将设置语言、时区和日期格式。": "이 지역은 언어, 시간대, 날짜 형식을 설정합니다.",
	},
	"fr-FR": {
		"开始": "Démarrer", "程序(P)": "Programmes", "文档(D)": "Documents", "设置(S)": "Paramètres",
		"查找(F)": "Rechercher", "帮助(H)": "Aide", "运行(R)…": "Exécuter…",
		"注销(L)…": "Fermer la session…", "重新启动(R)…": "Redémarrer…", "关闭计算机(U)…": "Arrêter…",
		"睡眠(S)…": "Veille…", "我的电脑": "Poste de travail", "我的文档": "Mes documents",
		"网络浏览器": "Navigateur Web", "方块世界 95": "Block World 95",
		"XFCE 终端": "Terminal XFCE", "XFCE 桌面": "Bureau XFCE", "回收站": "Corbeille", "注册表编辑器": "Éditeur du Registre",
		"记事本": "Bloc-notes", "计算器": "Calculatrice", "日历": "Calendrier", "终端": "Terminal",
		"文件管理器": "Explorateur", "纸牌": "Solitaire", "扫雷": "Démineur", "画图": "Paint",
		"图片查看器": "Visionneuse d'images", "图形化下载器": "Téléchargeur",
		"软件包管理器": "Gestionnaire de paquets", "屏幕键盘": "Clavier visuel",
		"系统信息": "Informations système", "设置中心": "Paramètres", "关于本机": "À propos",
		"显示属性(D)": "Propriétés de l'affichage", "主题": "Thème", "语言": "Langue",
		"字体": "Polices", "时间": "Heure", "地区": "Région", "关于": "À propos",
		"许可协议": "Licence", "确定": "OK", "取消": "Annuler", "应用": "Appliquer",
		"关闭": "Fermer", "打开": "Ouvrir", "新游戏": "Nouvelle partie", "开始游戏": "Jouer",
		"选择界面语言，应用后立即生效。": "Choisissez la langue d'affichage. Effet immédiat.",
		"选择主题，应用后立即生效。":   "Choisissez un thème. Effet immédiat.",
		"时区": "Fuseau horaire", "日期格式": "Format de date", "时间格式": "Format d'heure",
		"24 小时制": "Horloge 24 heures", "一周开始": "Premier jour de la semaine",
		"星期一": "Lundi", "星期日": "Dimanche", "当前时间": "Heure actuelle",
		"字体预览": "Aperçu des polices", "字体覆盖": "Couverture des polices",
		"世界语言与字体已全部安装。": "Toutes les polices des langues du monde sont installées.",
		"版本": "Version", "系统": "Système", "许可证": "Licence",
		"本系统包含的第三方组件：": "Composants tiers inclus :",
		"界面语言":         "Langue d'affichage", "桌面与窗口颜色": "Couleurs du bureau et des fenêtres",
		"显示": "Affichage", "打开显示属性…": "Ouvrir les propriétés d'affichage…",
		"语言与格式": "Langue et formats", "区域预设": "Préréglages régionaux",
		"应用区域设置": "Appliquer les paramètres régionaux",
		"此区域将设置语言、时区和日期格式。": "Cette région définit la langue, le fuseau horaire et le format de date.",
	},
	"de-DE": {
		"开始": "Start", "程序(P)": "Programme", "文档(D)": "Dokumente", "设置(S)": "Einstellungen",
		"查找(F)": "Suchen", "帮助(H)": "Hilfe", "运行(R)…": "Ausführen…",
		"注销(L)…": "Abmelden…", "重新启动(R)…": "Neu starten…", "关闭计算机(U)…": "Herunterfahren…",
		"睡眠(S)…": "Energiesparen…", "我的电脑": "Arbeitsplatz", "我的文档": "Eigene Dateien",
		"网络浏览器": "Webbrowser", "方块世界 95": "Blockwelt 95",
		"XFCE 终端": "XFCE-Terminal", "XFCE 桌面": "XFCE-Desktop", "回收站": "Papierkorb", "注册表编辑器": "Registrierungseditor",
		"记事本": "Editor", "计算器": "Rechner", "日历": "Kalender", "终端": "Terminal",
		"文件管理器": "Dateimanager", "纸牌": "Solitär", "扫雷": "Minensucher", "画图": "Paint",
		"图片查看器": "Bildbetrachter", "图形化下载器": "Download-Manager",
		"软件包管理器": "Paketmanager", "屏幕键盘": "Bildschirmtastatur",
		"系统信息": "Systeminformationen", "设置中心": "Einstellungen", "关于本机": "Info über",
		"显示属性(D)": "Anzeigeeigenschaften", "主题": "Design", "语言": "Sprache",
		"字体": "Schriftarten", "时间": "Uhrzeit", "地区": "Region", "关于": "Über",
		"许可协议": "Lizenz", "确定": "OK", "取消": "Abbrechen", "应用": "Übernehmen",
		"关闭": "Schließen", "打开": "Öffnen", "新游戏": "Neues Spiel", "开始游戏": "Spiel starten",
		"选择界面语言，应用后立即生效。": "Anzeigesprache wählen. Änderung sofort wirksam.",
		"选择主题，应用后立即生效。":   "Design wählen. Änderung sofort wirksam.",
		"时区": "Zeitzone", "日期格式": "Datumsformat", "时间格式": "Zeitformat",
		"24 小时制": "24-Stunden-Anzeige", "一周开始": "Erster Wochentag",
		"星期一": "Montag", "星期日": "Sonntag", "当前时间": "Aktuelle Zeit",
		"字体预览": "Schriftvorschau", "字体覆盖": "Schriftabdeckung",
		"世界语言与字体已全部安装。": "Alle Schriftarten der Weltsprachen sind installiert.",
		"版本": "Version", "系统": "System", "许可证": "Lizenz",
		"本系统包含的第三方组件：": "Enthaltene Komponenten von Drittanbietern:",
		"界面语言":         "Anzeigesprache", "桌面与窗口颜色": "Desktop- und Fensterfarben",
		"显示": "Anzeige", "打开显示属性…": "Anzeigeeigenschaften öffnen…",
		"语言与格式": "Sprache und Formate", "区域预设": "Regionsvorgaben",
		"应用区域设置": "Regionseinstellungen übernehmen",
		"此区域将设置语言、时区和日期格式。": "Diese Region legt Sprache, Zeitzone und Datumsformat fest.",
	},
	"es-ES": {
		"开始": "Inicio", "程序(P)": "Programas", "文档(D)": "Documentos", "设置(S)": "Configuración",
		"查找(F)": "Buscar", "帮助(H)": "Ayuda", "运行(R)…": "Ejecutar…",
		"注销(L)…": "Cerrar sesión…", "重新启动(R)…": "Reiniciar…", "关闭计算机(U)…": "Apagar…",
		"睡眠(S)…": "Suspender…", "我的电脑": "Mi PC", "我的文档": "Mis documentos",
		"网络浏览器": "Navegador web", "方块世界 95": "Mundo de bloques 95",
		"XFCE 终端": "Terminal XFCE", "XFCE 桌面": "Escritorio XFCE", "回收站": "Papelera", "注册表编辑器": "Editor del Registro",
		"记事本": "Bloc de notas", "计算器": "Calculadora", "日历": "Calendario", "终端": "Terminal",
		"文件管理器": "Explorador de archivos", "纸牌": "Solitario", "扫雷": "Buscaminas",
		"画图": "Paint", "图片查看器": "Visor de imágenes", "图形化下载器": "Gestor de descargas",
		"软件包管理器": "Gestor de paquetes", "屏幕键盘": "Teclado en pantalla",
		"系统信息": "Información del sistema", "设置中心": "Configuración", "关于本机": "Acerca de",
		"显示属性(D)": "Propiedades de pantalla", "主题": "Tema", "语言": "Idioma",
		"字体": "Fuentes", "时间": "Hora", "地区": "Región", "关于": "Acerca de",
		"许可协议": "Licencia", "确定": "Aceptar", "取消": "Cancelar", "应用": "Aplicar",
		"关闭": "Cerrar", "打开": "Abrir", "新游戏": "Nueva partida", "开始游戏": "Jugar",
		"选择界面语言，应用后立即生效。": "Elija el idioma de la interfaz. Se aplica al instante.",
		"选择主题，应用后立即生效。":   "Elija un tema. Se aplica al instante.",
		"时区": "Zona horaria", "日期格式": "Formato de fecha", "时间格式": "Formato de hora",
		"24 小时制": "Reloj de 24 horas", "一周开始": "Primer día de la semana",
		"星期一": "Lunes", "星期日": "Domingo", "当前时间": "Hora actual",
		"字体预览": "Vista previa de fuentes", "字体覆盖": "Cobertura de fuentes",
		"世界语言与字体已全部安装。": "Todas las fuentes de idiomas del mundo están instaladas.",
		"版本": "Versión", "系统": "Sistema", "许可证": "Licencia",
		"本系统包含的第三方组件：": "Componentes de terceros incluidos:",
		"界面语言":         "Idioma de la interfaz", "桌面与窗口颜色": "Colores del escritorio y ventanas",
		"显示": "Pantalla", "打开显示属性…": "Abrir propiedades de pantalla…",
		"语言与格式": "Idioma y formatos", "区域预设": "Ajustes regionales",
		"应用区域设置": "Aplicar ajustes regionales",
		"此区域将设置语言、时区和日期格式。": "Esta región define el idioma, la zona horaria y el formato de fecha.",
	},
	"ru-RU": {
		"开始": "Пуск", "程序(P)": "Программы", "文档(D)": "Документы", "设置(S)": "Настройка",
		"查找(F)": "Найти", "帮助(H)": "Справка", "运行(R)…": "Выполнить…",
		"注销(L)…": "Выйти…", "重新启动(R)…": "Перезагрузить…", "关闭计算机(U)…": "Завершить работу…",
		"睡眠(S)…": "Сон…", "我的电脑": "Мой компьютер", "我的文档": "Мои документы",
		"网络浏览器": "Веб-браузер", "方块世界 95": "Блочный мир 95",
		"XFCE 终端": "Терминал XFCE", "XFCE 桌面": "Рабочий стол XFCE", "回收站": "Корзина", "注册表编辑器": "Редактор реестра",
		"记事本": "Блокнот", "计算器": "Калькулятор", "日历": "Календарь", "终端": "Терминал",
		"文件管理器": "Проводник", "纸牌": "Косынка", "扫雷": "Сапёр", "画图": "Paint",
		"图片查看器": "Просмотр изображений", "图形化下载器": "Менеджер загрузок",
		"软件包管理器": "Менеджер пакетов", "屏幕键盘": "Экранная клавиатура",
		"系统信息": "Сведения о системе", "设置中心": "Параметры", "关于本机": "О системе",
		"显示属性(D)": "Свойства экрана", "主题": "Тема", "语言": "Язык", "字体": "Шрифты",
		"时间": "Время", "地区": "Регион", "关于": "О программе", "许可协议": "Лицензия",
		"确定": "ОК", "取消": "Отмена", "应用": "Применить", "关闭": "Закрыть",
		"打开": "Открыть", "新游戏": "Новая игра", "开始游戏": "Начать игру",
		"选择界面语言，应用后立即生效。": "Выберите язык интерфейса. Изменения применяются сразу.",
		"选择主题，应用后立即生效。":   "Выберите тему. Изменения применяются сразу.",
		"时区": "Часовой пояс", "日期格式": "Формат даты", "时间格式": "Формат времени",
		"24 小时制": "24-часовой формат", "一周开始": "Первый день недели",
		"星期一": "Понедельник", "星期日": "Воскресенье", "当前时间": "Текущее время",
		"字体预览": "Предпросмотр шрифтов", "字体覆盖": "Покрытие шрифтов",
		"世界语言与字体已全部安装。": "Установлены шрифты всех языков мира.",
		"版本": "Версия", "系统": "Система", "许可证": "Лицензия",
		"本系统包含的第三方组件：": "Включённые сторонние компоненты:",
		"界面语言":         "Язык интерфейса", "桌面与窗口颜色": "Цвета рабочего стола и окон",
		"显示": "Экран", "打开显示属性…": "Открыть свойства экрана…",
		"语言与格式": "Язык и форматы", "区域预设": "Региональные настройки",
		"应用区域设置": "Применить региональные настройки",
		"此区域将设置语言、时区和日期格式。": "Этот регион задаёт язык, часовой пояс и формат даты.",
	},
	"pt-BR": {
		"开始": "Iniciar", "程序(P)": "Programas", "文档(D)": "Documentos", "设置(S)": "Configurações",
		"查找(F)": "Localizar", "帮助(H)": "Ajuda", "运行(R)…": "Executar…",
		"注销(L)…": "Sair…", "重新启动(R)…": "Reiniciar…", "关闭计算机(U)…": "Desligar…",
		"睡眠(S)…": "Suspender…", "我的电脑": "Meu computador", "我的文档": "Meus documentos",
		"网络浏览器": "Navegador", "方块世界 95": "Mundo de Blocos 95",
		"XFCE 终端": "Terminal XFCE", "XFCE 桌面": "Área de trabalho XFCE", "回收站": "Lixeira", "注册表编辑器": "Editor do Registro",
		"记事本": "Bloco de notas", "计算器": "Calculadora", "日历": "Calendário", "终端": "Terminal",
		"文件管理器": "Gerenciador de arquivos", "纸牌": "Paciência", "扫雷": "Campo minado",
		"画图": "Paint", "图片查看器": "Visualizador de imagens", "图形化下载器": "Gerenciador de downloads",
		"软件包管理器": "Gerenciador de pacotes", "屏幕键盘": "Teclado virtual",
		"系统信息": "Informações do sistema", "设置中心": "Configurações", "关于本机": "Sobre",
		"显示属性(D)": "Propriedades de vídeo", "主题": "Tema", "语言": "Idioma",
		"字体": "Fontes", "时间": "Hora", "地区": "Região", "关于": "Sobre",
		"许可协议": "Licença", "确定": "OK", "取消": "Cancelar", "应用": "Aplicar",
		"关闭": "Fechar", "打开": "Abrir", "新游戏": "Novo jogo", "开始游戏": "Jogar",
		"选择界面语言，应用后立即生效。": "Escolha o idioma da interface. A alteração é imediata.",
		"选择主题，应用后立即生效。":   "Escolha um tema. A alteração é imediata.",
		"时区": "Fuso horário", "日期格式": "Formato de data", "时间格式": "Formato de hora",
		"24 小时制": "Relógio de 24 horas", "一周开始": "Primeiro dia da semana",
		"星期一": "Segunda-feira", "星期日": "Domingo", "当前时间": "Hora atual",
		"字体预览": "Prévia das fontes", "字体覆盖": "Cobertura das fontes",
		"世界语言与字体已全部安装。": "Todas as fontes de idiomas do mundo estão instaladas.",
		"版本": "Versão", "系统": "Sistema", "许可证": "Licença",
		"本系统包含的第三方组件：": "Componentes de terceiros incluídos:",
		"界面语言":         "Idioma da interface", "桌面与窗口颜色": "Cores da área de trabalho e janelas",
		"显示": "Tela", "打开显示属性…": "Abrir propriedades de vídeo…",
		"语言与格式": "Idioma e formatos", "区域预设": "Predefinições regionais",
		"应用区域设置": "Aplicar configurações regionais",
		"此区域将设置语言、时区和日期格式。": "Esta região define o idioma, o fuso horário e o formato de data.",
	},
	"it-IT": {
		"开始": "Avvio", "程序(P)": "Programmi", "文档(D)": "Documenti", "设置(S)": "Impostazioni",
		"查找(F)": "Trova", "帮助(H)": "Guida", "运行(R)…": "Esegui…",
		"注销(L)…": "Disconnetti…", "重新启动(R)…": "Riavvia…", "关闭计算机(U)…": "Spegni…",
		"睡眠(S)…": "Sospendi…", "我的电脑": "Risorse del computer", "我的文档": "Documenti",
		"网络浏览器": "Browser web", "方块世界 95": "Mondo a blocchi 95",
		"XFCE 终端": "Terminale XFCE", "XFCE 桌面": "Desktop XFCE", "回收站": "Cestino", "注册表编辑器": "Editor del Registro",
		"记事本": "Blocco note", "计算器": "Calcolatrice", "日历": "Calendario", "终端": "Terminale",
		"文件管理器": "Esplora file", "纸牌": "Solitario", "扫雷": "Campo minato",
		"画图": "Paint", "图片查看器": "Visualizzatore immagini", "图形化下载器": "Gestore download",
		"软件包管理器": "Gestore pacchetti", "屏幕键盘": "Tastiera a schermo",
		"系统信息": "Informazioni di sistema", "设置中心": "Impostazioni", "关于本机": "Informazioni",
		"显示属性(D)": "Proprietà schermo", "主题": "Tema", "语言": "Lingua",
		"字体": "Caratteri", "时间": "Ora", "地区": "Area", "关于": "Informazioni",
		"许可协议": "Licenza", "确定": "OK", "取消": "Annulla", "应用": "Applica",
		"关闭": "Chiudi", "打开": "Apri", "新游戏": "Nuova partita", "开始游戏": "Gioca",
		"选择界面语言，应用后立即生效。": "Scegli la lingua dell'interfaccia. Ha effetto immediato.",
		"选择主题，应用后立即生效。":   "Scegli un tema. Ha effetto immediato.",
		"时区": "Fuso orario", "日期格式": "Formato data", "时间格式": "Formato ora",
		"24 小时制": "Orologio a 24 ore", "一周开始": "Primo giorno della settimana",
		"星期一": "Lunedì", "星期日": "Domenica", "当前时间": "Ora corrente",
		"字体预览": "Anteprima caratteri", "字体覆盖": "Copertura caratteri",
		"世界语言与字体已全部安装。": "Tutti i caratteri delle lingue del mondo sono installati.",
		"版本": "Versione", "系统": "Sistema", "许可证": "Licenza",
		"本系统包含的第三方组件：": "Componenti di terze parti inclusi:",
		"界面语言":         "Lingua dell'interfaccia", "桌面与窗口颜色": "Colori di desktop e finestre",
		"显示": "Schermo", "打开显示属性…": "Apri proprietà schermo…",
		"语言与格式": "Lingua e formati", "区域预设": "Preset regionali",
		"应用区域设置": "Applica impostazioni regionali",
		"此区域将设置语言、时区和日期格式。": "Questa area imposta lingua, fuso orario e formato data.",
	},
}

// dateFormatChoices are Go layouts plus a human readable sample.
type dateFormatChoice struct {
	Layout string
	Label  string
}

var dateFormatChoices = []dateFormatChoice{
	{"2006-01-02", "YYYY-MM-DD"},
	{"02/01/2006", "DD/MM/YYYY"},
	{"01/02/2006", "MM/DD/YYYY"},
	{"2006年1月2日", "YYYY年M月D日"},
	{"02.01.2006", "DD.MM.YYYY"},
	{"2006/01/02", "YYYY/MM/DD"},
}

type zoneChoice struct {
	ID   string
	City string
}

// worldZones is a curated list of the world's major time zones.
var worldZones = []zoneChoice{
	{"Pacific/Auckland", "奥克兰"},
	{"Australia/Sydney", "悉尼"},
	{"Australia/Melbourne", "墨尔本"},
	{"Australia/Brisbane", "布里斯班"},
	{"Australia/Adelaide", "阿德莱德"},
	{"Australia/Perth", "珀斯"},
	{"Asia/Tokyo", "东京"},
	{"Asia/Seoul", "首尔"},
	{"Asia/Shanghai", "北京 / 上海"},
	{"Asia/Hong_Kong", "香港"},
	{"Asia/Taipei", "台北"},
	{"Asia/Singapore", "新加坡"},
	{"Asia/Kuala_Lumpur", "吉隆坡"},
	{"Asia/Manila", "马尼拉"},
	{"Asia/Jakarta", "雅加达"},
	{"Asia/Bangkok", "曼谷"},
	{"Asia/Ho_Chi_Minh", "胡志明市"},
	{"Asia/Phnom_Penh", "金边"},
	{"Asia/Yangon", "仰光"},
	{"Asia/Dhaka", "达卡"},
	{"Asia/Kathmandu", "加德满都"},
	{"Asia/Kolkata", "新德里 / 加尔各答"},
	{"Asia/Karachi", "卡拉奇"},
	{"Asia/Dubai", "迪拜"},
	{"Asia/Riyadh", "利雅得"},
	{"Asia/Qatar", "多哈"},
	{"Asia/Tehran", "德黑兰"},
	{"Asia/Jerusalem", "耶路撒冷"},
	{"Asia/Istanbul", "伊斯坦布尔"},
	{"Asia/Almaty", "阿拉木图"},
	{"Asia/Tashkent", "塔什干"},
	{"Asia/Baku", "巴库"},
	{"Asia/Colombo", "科伦坡"},
	{"Asia/Ulaanbaatar", "乌兰巴托"},
	{"Europe/Moscow", "莫斯科"},
	{"Europe/Kyiv", "基辅"},
	{"Europe/Istanbul", "伊斯坦布尔（欧洲）"},
	{"Europe/Athens", "雅典"},
	{"Europe/Helsinki", "赫尔辛基"},
	{"Europe/Riga", "里加"},
	{"Europe/Tallinn", "塔林"},
	{"Europe/Vilnius", "维尔纽斯"},
	{"Europe/Warsaw", "华沙"},
	{"Europe/Prague", "布拉格"},
	{"Europe/Budapest", "布达佩斯"},
	{"Europe/Vienna", "维也纳"},
	{"Europe/Zurich", "苏黎世"},
	{"Europe/Berlin", "柏林"},
	{"Europe/Rome", "罗马"},
	{"Europe/Paris", "巴黎"},
	{"Europe/Madrid", "马德里"},
	{"Europe/Lisbon", "里斯本"},
	{"Europe/London", "伦敦"},
	{"Europe/Dublin", "都柏林"},
	{"Europe/Amsterdam", "阿姆斯特丹"},
	{"Europe/Brussels", "布鲁塞尔"},
	{"Europe/Copenhagen", "哥本哈根"},
	{"Europe/Oslo", "奥斯陆"},
	{"Europe/Stockholm", "斯德哥尔摩"},
	{"Europe/Bucharest", "布加勒斯特"},
	{"Europe/Sofia", "索非亚"},
	{"Europe/Belgrade", "贝尔格莱德"},
	{"Europe/Zagreb", "萨格勒布"},
	{"Africa/Cairo", "开罗"},
	{"Africa/Johannesburg", "约翰内斯堡"},
	{"Africa/Nairobi", "内罗毕"},
	{"Africa/Lagos", "拉各斯"},
	{"Africa/Accra", "阿克拉"},
	{"Africa/Casablanca", "卡萨布兰卡"},
	{"Africa/Tunis", "突尼斯"},
	{"Africa/Algiers", "阿尔及尔"},
	{"Africa/Addis_Ababa", "亚的斯亚贝巴"},
	{"Africa/Dakar", "达喀尔"},
	{"America/St_Johns", "圣约翰斯"},
	{"America/Halifax", "哈利法克斯"},
	{"America/New_York", "纽约"},
	{"America/Toronto", "多伦多"},
	{"America/Chicago", "芝加哥"},
	{"America/Mexico_City", "墨西哥城"},
	{"America/Denver", "丹佛"},
	{"America/Phoenix", "菲尼克斯"},
	{"America/Los_Angeles", "洛杉矶"},
	{"America/Vancouver", "温哥华"},
	{"America/Anchorage", "安克雷奇"},
	{"Pacific/Honolulu", "檀香山"},
	{"America/Bogota", "波哥大"},
	{"America/Lima", "利马"},
	{"America/Santiago", "圣地亚哥"},
	{"America/Argentina/Buenos_Aires", "布宜诺斯艾利斯"},
	{"America/Sao_Paulo", "圣保罗"},
	{"America/Caracas", "加拉加斯"},
	{"America/Panama", "巴拿马城"},
	{"America/Havana", "哈瓦那"},
	{"Pacific/Guam", "关岛"},
	{"Pacific/Port_Moresby", "莫尔兹比港"},
	{"Pacific/Fiji", "苏瓦"},
	{"Pacific/Tahiti", "帕皮提"},
	{"UTC", "协调世界时 UTC"},
}

func (d *Desktop) zoneLabel(z zoneChoice) string {
	loc, err := time.LoadLocation(z.ID)
	if err != nil {
		return z.City + " (" + z.ID + ")"
	}
	_, off := time.Now().In(loc).Zone()
	sign := "+"
	if off < 0 {
		sign = "-"
		off = -off
	}
	return fmt.Sprintf("%s (UTC%s%02d:%02d)", z.City, sign, off/3600, (off%3600)/60)
}

func (d *Desktop) languageDef() languageDef {
	return desktopLanguages[languageIndex(d.lang)]
}

func (d *Desktop) dateFormat() dateFormatChoice {
	if d.dateFmt >= 0 && d.dateFmt < len(dateFormatChoices) {
		return dateFormatChoices[d.dateFmt]
	}
	return dateFormatChoices[0]
}

func (d *Desktop) formatDate(t time.Time) string {
	return t.In(d.clockLoc).Format(d.dateFormat().Layout)
}

func (d *Desktop) formatClock(t time.Time) string {
	local := t.In(d.clockLoc)
	if d.clock12 {
		return local.Format("3:04 PM")
	}
	return local.Format("15:04")
}

// applyLanguage switches the UI language, date format and (optionally) the
// 12-hour clock, then refreshes the desktop labels.
func (d *Desktop) applyLanguage(code string) {
	d.lang = desktopLanguages[languageIndex(code)].Code
	def := d.languageDef()
	d.dateFmt = indexDateFormat(def.DateLayout)
	if d.dateFmt < 0 {
		d.dateFmt = 0
	}
	d.clock12 = def.Clock12
	d.refreshIconLabels()
	d.clock = d.formatClock(time.Now())
	d.savePrefs()
	d.Invalidate()
}

func indexDateFormat(layout string) int {
	for i, c := range dateFormatChoices {
		if c.Layout == layout {
			return i
		}
	}
	return -1
}

func (d *Desktop) refreshIconLabels() {
	apply := func(ic *desktopIcon) {
		if ic.labelKey != "" {
			ic.label = d.tr(ic.labelKey)
		}
	}
	for _, ic := range d.icons {
		apply(ic)
	}
	for _, ic := range d.baseIcons {
		apply(ic)
	}
}

// regionPreset bundles a language, time zone and date format.
type regionPreset struct {
	Name    string
	Lang    string
	Zone    string
	DateFmt int
	Clock12 bool
	Monday  bool
}

var regionPresets = []regionPreset{
	{"中国大陆", "zh-CN", "Asia/Shanghai", 0, false, true},
	{"台湾地区", "zh-TW", "Asia/Taipei", 0, false, false},
	{"香港", "zh-TW", "Asia/Hong_Kong", 0, false, false},
	{"日本", "ja-JP", "Asia/Tokyo", 5, false, false},
	{"韩国", "ko-KR", "Asia/Seoul", 0, false, true},
	{"美国（东部）", "en-US", "America/New_York", 2, true, false},
	{"美国（太平洋）", "en-US", "America/Los_Angeles", 2, true, false},
	{"英国", "en-US", "Europe/London", 1, false, true},
	{"法国", "fr-FR", "Europe/Paris", 1, false, true},
	{"德国", "de-DE", "Europe/Berlin", 4, false, true},
	{"西班牙", "es-ES", "Europe/Madrid", 1, false, true},
	{"意大利", "it-IT", "Europe/Rome", 1, false, true},
	{"俄罗斯", "ru-RU", "Europe/Moscow", 4, false, true},
	{"巴西", "pt-BR", "America/Sao_Paulo", 1, false, false},
	{"澳大利亚", "en-US", "Australia/Sydney", 1, false, false},
	{"印度", "en-US", "Asia/Kolkata", 1, false, true},
	{"新加坡", "en-US", "Asia/Singapore", 1, false, true},
	{"阿联酋", "en-US", "Asia/Dubai", 1, false, false},
	{"南非", "en-US", "Africa/Johannesburg", 1, false, true},
}

func (d *Desktop) applyRegion(i int) {
	if i < 0 || i >= len(regionPresets) {
		return
	}
	r := regionPresets[i]
	d.lang = desktopLanguages[languageIndex(r.Lang)].Code
	if loc, err := time.LoadLocation(r.Zone); err == nil {
		d.clockLoc = loc
		d.clockZone = r.Zone
	}
	d.dateFmt = r.DateFmt
	d.clock12 = r.Clock12
	d.weekMonday = r.Monday
	if tbl := uiTranslations[d.lang]; tbl != nil || d.lang == "zh-CN" {
		d.refreshIconLabels()
	}
	osSetLocale(d.lang)
	d.clock = d.formatClock(time.Now())
	d.savePrefs()
	d.Invalidate()
}

// osSetLocale exposes the selection to child processes (terminal, XFCE).
func osSetLocale(code string) {
	locale := strings.ReplaceAll(code, "-", "_") + ".UTF-8"
	_ = os.Setenv("LANG", locale)
	_ = os.Setenv("LC_TIME", locale)
}

func (d *Desktop) dateSample() string {
	now := time.Now().In(d.clockLoc)
	return d.formatDate(now) + "  " + d.formatClock(now)
}
