package desktop

var v266ChatExportTranslations = map[string]map[string]string{
	"en-US": {
		"AI 助手对话记录\n": "AI Assistant Conversation\n",
		"你：":          "You:",
		"赛博义体：":       "Cybernetic Companion:",
	},
	"zh-TW": {
		"AI 助手对话记录\n": "AI 助手對話記錄\n",
		"你：":          "你：",
		"赛博义体：":       "賽博義體：",
	},
	"ja-JP": {
		"AI 助手对话记录\n": "AI アシスタント会話記録\n",
		"你：":          "あなた：",
		"赛博义体：":       "サイバー義体：",
	},
	"ko-KR": {
		"AI 助手对话记录\n": "AI 어시스턴트 대화 기록\n",
		"你：":          "당신:",
		"赛博义体：":       "사이버 의체:",
	},
	"fr-FR": {
		"AI 助手对话记录\n": "Historique de conversation avec l’assistant IA\n",
		"你：":          "Vous :",
		"赛博义体：":       "Cyborg :",
	},
	"de-DE": {
		"AI 助手对话记录\n": "KI-Assistent – Gesprächsprotokoll\n",
		"你：":          "Sie:",
		"赛博义体：":       "Cyborg:",
	},
	"es-ES": {
		"AI 助手对话记录\n": "Registro de conversación con el asistente de IA\n",
		"你：":          "Tú:",
		"赛博义体：":       "Ciborg:",
	},
	"ru-RU": {
		"AI 助手对话记录\n": "История диалога с ИИ-помощником\n",
		"你：":          "Вы:",
		"赛博义体：":       "Киборг:",
	},
	"pt-BR": {
		"AI 助手对话记录\n": "Registro de conversa com o assistente de IA\n",
		"你：":          "Você:",
		"赛博义体：":       "Corpo cibernético:",
	},
	"it-IT": {
		"AI 助手对话记录\n": "Registro della conversazione con l’assistente IA\n",
		"你：":          "Tu:",
		"赛博义体：":       "Corpo cibernetico:",
	},
}

func init() {
	for code, translations := range v266ChatExportTranslations {
		if extraTranslations[code] == nil {
			extraTranslations[code] = make(map[string]string)
		}
		for source, translated := range translations {
			extraTranslations[code][source] = translated
		}
	}
}
