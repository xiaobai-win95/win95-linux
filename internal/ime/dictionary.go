package ime

import (
	"fmt"
	"strconv"
	"strings"
)

type dictionaryEntry struct {
	text      string
	pinyin    string
	letters   string
	syllables []string
	frequency int
}

// compactDictionary is deliberately self-contained: text, keyboard pinyin, and
// relative frequency. The entries cover everyday language, computing, and the
// Windows 95 desktop vocabulary without requiring a system dictionary or cgo.
const compactDictionary = `
的|de|10000
一|yi|9900
是|shi|9800
不|bu|9700
了|le|9600
人|ren|9500
我|wo|9400
在|zai|9300
有|you|9200
他|ta|9100
这|zhe|9000
个|ge|8900
们|men|8800
中|zhong|9900
大|da|8700
到|dao|8600
上|shang|8500
说|shuo|8400
国|guo|8300
和|he|8200
地|di|8100
也|ye|8000
子|zi|7900
时|shi|7800
道|dao|7700
出|chu|7600
而|er|7500
于|yu|7400
发|fa|7300
下|xia|7200
得|de|7100
里|li|7000
后|hou|6900
自|zi|6800
之|zhi|6700
年|nian|6600
过|guo|6500
家|jia|6400
学|xue|6300
对|dui|6200
可|ke|6100
她|ta|6000
心|xin|5900
多|duo|5800
天|tian|5700
都|dou|5600
小|xiao|5500
但|dan|5400
现|xian|5300
面|mian|5200
前|qian|5100
法|fa|5000
当|dang|4900
与|yu|4800
还|huan|4700
看|kan|4600
将|jiang|4500
无|wu|4400
开|kai|4300
为|wei|4200
手|shou|4100
文|wen|4000
用|yong|3900
主|zhu|3800
能|neng|3700
你|ni|9700
您|nin|8000
它|ta|6500
大家|da jia|9000
自己|zi ji|8900
别人|bie ren|6500
朋友|peng you|9200
家人|jia ren|7000
爸爸|ba ba|8500
妈妈|ma ma|8600
哥哥|ge ge|7800
姐姐|jie jie|7900
弟弟|di di|7400
妹妹|mei mei|7500
孩子|hai zi|8800
老师|lao shi|9300
学生|xue sheng|9200
同事|tong shi|7600
男人|nan ren|7200
女人|nv ren|7300
先生|xian sheng|8000
女士|nv shi|7100
我们|wo men|9500
你们|ni men|9400
他们|ta men|9300
她们|ta men|8500
你好|ni hao|10000
您好|nin hao|9300
你好吗|ni hao ma|9000
谢谢|xie xie|9800
不客气|bu ke qi|8500
对不起|dui bu qi|9000
没关系|mei guan xi|8800
再见|zai jian|9000
欢迎|huan ying|8700
请|qing|9000
请问|qing wen|8600
是的|shi de|8200
不是|bu shi|9000
好的|hao de|8800
可以|ke yi|9300
不可以|bu ke yi|7000
没有|mei you|9500
不在|bu zai|6800
去|qu|9000
来|lai|9300
看|kan|9100
听|ting|9000
说|shuo|9200
读|du|7500
写|xie|9100
学|xue|8800
学习|xue xi|9600
工作|gong zuo|9500
休息|xiu xi|8200
睡觉|shui jiao|8400
起床|qi chuang|7000
吃饭|chi fan|8800
喝水|he shui|7800
走路|zou lu|6900
跑步|pao bu|7000
开车|kai che|7300
坐车|zuo che|6900
买|mai|8500
卖|mai|7200
给|gei|8800
拿|na|7300
用|yong|9000
找|zhao|8600
想|xiang|9000
知道|zhi dao|9300
认识|ren shi|8500
喜欢|xi huan|9400
爱|ai|9200
需要|xu yao|9000
会|hui|8800
要|yao|9200
好|hao|9500
坏|huai|7200
大|da|9000
小|xiao|8800
多|duo|8600
少|shao|7600
高|gao|8400
低|di|7600
长|chang|8000
短|duan|7300
新|xin|8800
旧|jiu|6900
快|kuai|8700
慢|man|7500
热|re|8000
冷|leng|7800
漂亮|piao liang|8200
高兴|gao xing|8300
快乐|kuai le|8400
伤心|shang xin|6800
累|lei|7100
忙|mang|7500
正确|zheng que|7600
错误|cuo wu|7800
简单|jian dan|8300
困难|kun nan|7500
重要|zhong yao|8500
方便|fang bian|8100
安全|an quan|8400
健康|jian kang|8500
如果|ru guo|8200
因为|yin wei|8600
所以|suo yi|8500
但是|dan shi|8700
而且|er qie|7400
然后|ran hou|8200
已经|yi jing|8600
还是|hai shi|8300
或者|huo zhe|8000
这个|zhe ge|9000
那个|na ge|8500
这些|zhe xie|7800
那些|na xie|7300
什么|shen me|9300
怎么|zen me|9000
为什么|wei shen me|8700
哪里|na li|8200
这里|zhe li|8800
那里|na li|8000
零|ling|6000
二|er|8800
两|liang|8000
三|san|8700
四|si|8600
五|wu|8500
六|liu|8400
七|qi|8300
八|ba|8200
九|jiu|8100
十|shi|8800
百|bai|7500
千|qian|7200
万|wan|7400
亿|yi|6300
第一|di yi|8000
第二|di er|7800
第三|di san|7600
中国|zhong guo|10000
北京|bei jing|9500
上海|shang hai|9400
广州|guang zhou|8800
深圳|shen zhen|8600
杭州|hang zhou|8200
成都|cheng du|8300
南京|nan jing|8500
武汉|wu han|8100
西安|xi an|8500
香港|xiang gang|8800
澳门|ao men|7800
台湾|tai wan|9000
美国|mei guo|9300
英国|ying guo|8600
日本|ri ben|8900
韩国|han guo|8400
法国|fa guo|8200
德国|de guo|8100
世界|shi jie|9400
国家|guo jia|9200
城市|cheng shi|8500
农村|nong cun|7200
家里|jia li|8400
学校|xue xiao|9100
公司|gong si|9300
商店|shang dian|8000
医院|yi yuan|8700
银行|yin hang|8500
车站|che zhan|7900
机场|ji chang|8100
公园|gong yuan|8000
酒店|jiu dian|7600
房间|fang jian|8200
门口|men kou|6900
中文|zhong wen|10000
汉语|han yu|9800
普通话|pu tong hua|8700
中国人|zhong guo ren|9500
中华人民共和国|zhong hua ren min gong he guo|9000
简体中文|jian ti zhong wen|8800
繁体中文|fan ti zhong wen|8200
输入法|shu ru fa|10000
输入|shu ru|9700
输出|shu chu|8800
拼音|pin yin|9600
汉字|han zi|9500
字体|zi ti|9000
键盘|jian pan|9400
鼠标|shu biao|9300
电脑|dian nao|9800
计算机|ji suan ji|9600
笔记本|bi ji ben|8800
操作系统|cao zuo xi tong|9200
系统|xi tong|9500
软件|ruan jian|9600
硬件|ying jian|9000
程序|cheng xu|9300
文件|wen jian|9600
文件夹|wen jian jia|9000
目录|mu lu|8200
窗口|chuang kou|9400
对话框|dui hua kuang|8600
菜单|cai dan|9000
按钮|an niu|8700
图标|tu biao|8500
桌面|zhuo mian|9200
屏幕|ping mu|9000
显示|xian shi|9300
隐藏|yin cang|7800
搜索|sou suo|8900
查找|cha zhao|8700
复制|fu zhi|9300
粘贴|zhan tie|8800
剪切|jian qie|8400
撤销|che xiao|8200
保存|bao cun|9400
另存为|ling cun wei|8000
打开|da kai|9500
关闭|guan bi|9400
退出|tui chu|9000
启动|qi dong|8800
运行|yun xing|9100
帮助|bang zhu|8600
设置|she zhi|9200
控制面板|kong zhi mian ban|8500
资源管理器|zi yuan guan li qi|8400
回收站|hui shou zhan|8300
任务栏|ren wu lan|8200
开始菜单|kai shi cai dan|8100
快捷方式|kuai jie fang shi|8000
浏览器|liu lan qi|9000
互联网|hu lian wang|8900
网络|wang luo|9300
电子邮件|dian zi you jian|8600
聊天|liao tian|8800
游戏|you xi|9400
音乐|yin yue|9500
视频|shi pin|9100
照片|zhao pian|8800
图片|tu pian|9000
文档|wen dang|9200
记事本|ji shi ben|8600
画图|hua tu|8200
计算器|ji suan qi|8500
时钟|shi zhong|8000
日历|ri li|8100
天气|tian qi|9000
时间|shi jian|9500
日期|ri qi|8400
今天|jin tian|9600
明天|ming tian|9300
昨天|zuo tian|9000
现在|xian zai|9500
早上|zao shang|8700
中午|zhong wu|8300
晚上|wan shang|8900
星期|xing qi|8800
星期一|xing qi yi|8300
星期二|xing qi er|8000
星期三|xing qi san|7900
星期四|xing qi si|7700
星期五|xing qi wu|7800
星期六|xing qi liu|7600
星期日|xing qi ri|7500
windows|windows|9000
win95|win95|8500
windows95|windows95|10000
视窗95|shi chuang 95|8800
视窗|shi chuang|9000
微软|wei ruan|8600
米饭|mi fan|8000
面条|mian tiao|7900
饺子|jiao zi|8100
包子|bao zi|7600
面包|mian bao|8200
牛奶|niu nai|8300
咖啡|ka fei|8400
茶水|cha shui|6800
苹果|ping guo|8600
香蕉|xiang jiao|8300
西瓜|xi gua|8100
蔬菜|shu cai|7600
水果|shui guo|8500
早饭|zao fan|7300
午饭|wu fan|7400
晚饭|wan fan|7500
餐馆|can guan|7200
饭店|fan dian|8000
点菜|dian cai|7000
好吃|hao chi|8200
安|an|7800
按|an|7000
八|ba|8500
百|bai|8000
办|ban|8000
半|ban|7400
帮|bang|7600
包|bao|7500
报|bao|7800
杯|bei|6800
北|bei|8200
被|bei|8300
比|bi|8000
笔|bi|8100
必|bi|7400
边|bian|7600
变|bian|8000
表|biao|7500
别|bie|7900
病|bing|7200
步|bu|7000
部|bu|7300
才|cai|7600
菜|cai|7900
草|cao|6900
查|cha|7600
产|chan|7300
常|chang|8000
场|chang|7700
车|che|8100
成|cheng|8200
城|cheng|7600
吃|chi|8400
持|chi|6800
初|chu|7500
除|chu|7300
穿|chuan|7600
传|chuan|7400
船|chuan|6900
窗|chuang|8000
床|chuang|7400
春|chun|7700
词|ci|7100
从|cong|8000
村|cun|6900
存|cun|7200
错|cuo|7800
答|da|7400
打|da|8300
带|dai|7800
待|dai|7000
担|dan|6700
胆|dan|6500
弹|dan|6900
刀|dao|7000
导|dao|6900
倒|dao|7200
等|deng|8100
低|di|7600
底|di|7300
第|di|8000
点|dian|8400
电|dian|8600
店|dian|7500
掉|diao|7000
顶|ding|7200
东|dong|7900
冬|dong|6900
懂|dong|6800
读|du|7800
度|du|7300
短|duan|7500
段|duan|7200
饿|e|6500
儿|er|7900
翻|fan|7200
反|fan|7600
饭|fan|7700
房|fang|7400
放|fang|7900
非|fei|7100
飞|fei|7600
费|fei|7000
分|fen|8100
份|fen|6800
风|feng|7900
佛|fo|6100
服|fu|7300
福|fu|7200
父|fu|7000
付|fu|7400
负|fu|7000
妇|fu|6600
复|fu|7300
富|fu|7100
该|gai|7600
改|gai|7900
干|gan|7300
感|gan|7800
刚|gang|7600
钢|gang|6500
高|gao|8300
告|gao|7500
哥|ge|7200
歌|ge|7400
格|ge|6800
各|ge|7700
根|gen|7500
跟|gen|7800
更|geng|7300
工|gong|8000
公|gong|7900
功|gong|7000
共|gong|7100
够|gou|7200
古|gu|7500
谷|gu|6800
股|gu|6900
故|gu|7300
顾|gu|7000
关|guan|8000
观|guan|7400
管|guan|7600
光|guang|7900
广|guang|7100
归|gui|6900
鬼|gui|6800
贵|gui|7300
果|guo|7900
孩|hai|7200
海|hai|7800
喊|han|6500
汉|han|7400
好|hao|9000
号|hao|7100
喝|he|6900
何|he|7400
河|he|7500
很|hen|8600
红|hong|7900
厚|hou|6600
户|hu|6800
花|hua|8000
画|hua|7700
坏|huai|7300
欢|huan|7600
环|huan|7400
换|huan|7700
黄|huang|7800
回|hui|8000
会|hui|8500
活|huo|7900
火|huo|7600
机|ji|8000
级|ji|7300
极|ji|7400
急|ji|7800
集|ji|7500
己|ji|6800
记|ji|8000
计|ji|7900
技|ji|7700
加|jia|7900
假|jia|7400
价|jia|7600
架|jia|6900
间|jian|8000
简|jian|7300
见|jian|8200
件|jian|7800
建|jian|7900
健|jian|7400
讲|jiang|7400
降|jiang|6900
交|jiao|7600
叫|jiao|7800
接|jie|7900
街|jie|7300
节|jie|7600
姐|jie|7300
借|jie|7500
介|jie|6800
界|jie|7700
今|jin|7600
金|jin|7800
紧|jin|7200
尽|jin|7100
近|jin|8000
京|jing|7700
经|jing|8100
井|jing|6600
静|jing|7300
九|jiu|7800
久|jiu|7400
酒|jiu|7700
旧|jiu|7500
救|jiu|7200
就|jiu|8200
居|ju|6900
局|ju|7300
举|ju|7400
句|ju|6900
具|ju|7200
据|ju|7500
决|jue|7500
军|jun|7600
康|kang|7100
考|kao|7600
靠|kao|7300
科|ke|7600
刻|ke|7000
客|ke|7300
课|ke|7800
空|kong|7900
口|kou|7800
哭|ku|6900
苦|ku|7300
块|kuai|7200
宽|kuan|7000
况|kuang|6800
困|kun|7100
拉|la|7400
来|lai|8800
蓝|lan|7300
老|lao|8100
乐|le|7600
类|lei|7500
累|lei|7200
冷|leng|7700
历|li|7300
立|li|7600
利|li|7500
例|li|7200
连|lian|7600
联|lian|7500
脸|lian|7200
练|lian|7300
量|liang|7400
两|liang|7900
亮|liang|7600
列|lie|6900
林|lin|7600
临|lin|6900
领|ling|7500
令|ling|7000
留|liu|7400
流|liu|7800
六|liu|7900
龙|long|7400
楼|lou|7300
路|lu|8000
录|lu|7000
陆|lu|7100
旅|lv|7300
绿|lv|7400
乱|luan|7000
论|lun|7400
落|luo|7500
妈|ma|7800
吗|ma|7600
马|ma|7500
买|mai|8000
卖|mai|7300
满|man|7700
慢|man|7400
忙|mang|7600
猫|mao|7400
毛|mao|7300
帽|mao|6600
没|mei|8600
每|mei|8000
美|mei|8200
门|men|7900
梦|meng|7500
米|mi|7200
民|min|7300
明|ming|8000
名|ming|7900
命|ming|7300
模|mo|6900
末|mo|6700
莫|mo|6500
母|mu|7200
木|mu|7100
目|mu|6900
拿|na|7500
哪|na|7700
那|na|8200
奶|nai|6900
难|nan|7800
男|nan|7900
南|nan|7800
脑|nao|7300
闹|nao|6800
内|nei|7700
年|nian|8300
念|nian|7000
宁|ning|6900
牛|niu|7100
农|nong|7200
女|nv|7900
暖|nuan|6800
虐|nve|6200
哦|o|6100
欧|ou|6300
怕|pa|7300
拍|pai|7200
排|pai|7300
盘|pan|7000
旁|pang|6900
胖|pang|6700
跑|pao|7600
配|pei|7400
朋|peng|7100
批|pi|6800
皮|pi|7000
篇|pian|6600
片|pian|7400
票|piao|7200
品|pin|7000
平|ping|7800
评|ping|7000
瓶|ping|6900
破|po|7200
普|pu|6800
期|qi|7400
齐|qi|6800
其|qi|7200
奇|qi|7300
骑|qi|6900
起|qi|7900
气|qi|8000
器|qi|7300
钱|qian|7800
浅|qian|6900
强|qiang|7700
桥|qiao|7000
巧|qiao|7200
切|qie|7300
且|qie|6700
亲|qin|7400
轻|qing|7600
青|qing|7500
清|qing|7800
情|qing|7900
请|qing|8500
庆|qing|6800
穷|qiong|6600
秋|qiu|7100
求|qiu|7600
球|qiu|7400
取|qu|7700
去|qu|8200
圈|quan|7000
全|quan|8000
权|quan|7300
泉|quan|6500
却|que|7500
确|que|7300
群|qun|7400
然|ran|7600
让|rang|7800
绕|rao|6500
热|re|7900
认|ren|7800
任|ren|7300
日|ri|8000
容|rong|7200
肉|rou|7100
软|ruan|7600
瑞|rui|6500
三|san|8200
散|san|6800
色|se|7500
森|sen|6200
杀|sha|6900
山|shan|7900
闪|shan|6800
善|shan|7000
商|shang|7300
少|shao|7900
社|she|7100
设|she|7600
深|shen|7900
什|shen|7300
神|shen|7400
审|shen|6900
生|sheng|8100
声|sheng|7800
升|sheng|7000
省|sheng|7400
胜|sheng|7200
师|shi|7900
诗|shi|7000
失|shi|7200
十|shi|8800
识|shi|7500
实|shi|8300
食|shi|7600
史|shi|7200
使|shi|7900
始|shi|7300
世|shi|8000
市|shi|8100
事|shi|8200
视|shi|7700
试|shi|7900
室|shi|7200
收|shou|7800
守|shou|7000
首|shou|7400
受|shou|7600
书|shu|8000
输|shu|7300
熟|shu|7000
术|shu|7100
树|shu|7800
数|shu|7700
双|shuang|7300
谁|shui|7300
水|shui|8500
睡|shui|7600
司|si|6800
思|si|7400
死|si|7300
四|si|8000
送|song|7600
诉|su|6700
素|su|6900
速|su|7400
算|suan|7600
虽|sui|6600
岁|sui|6900
孙|sun|6800
台|tai|7600
太|tai|7800
态|tai|7300
谈|tan|7300
汤|tang|6900
糖|tang|6800
特|te|7500
疼|teng|6700
提|ti|7500
题|ti|7600
体|ti|7800
替|ti|7000
田|tian|6900
条|tiao|7400
跳|tiao|7600
铁|tie|7100
听|ting|8400
停|ting|7700
通|tong|7800
同|tong|8000
童|tong|6900
统|tong|7300
痛|tong|7000
头|tou|8000
投|tou|7300
图|tu|7500
突|tu|7000
土|tu|7200
团|tuan|6900
推|tui|7300
退|tui|7400
脱|tuo|6800
外|wai|8200
完|wan|7900
玩|wan|7800
晚|wan|7600
万|wan|7700
王|wang|7600
网|wang|8200
往|wang|7400
忘|wang|7000
望|wang|7300
危|wei|6900
围|wei|7000
维|wei|7200
伟|wei|6700
卫|wei|6900
未|wei|7300
位|wei|7600
味|wei|7000
温|wen|7300
闻|wen|7200
稳|wen|7100
问|wen|8500
握|wo|6900
卧|wo|6500
五|wu|8000
午|wu|7200
舞|wu|7100
物|wu|7900
误|wu|7000
西|xi|8000
希|xi|6900
吸|xi|7100
息|xi|7400
习|xi|7600
席|xi|6900
喜|xi|7500
细|xi|7300
戏|xi|7200
夏|xia|7300
先|xian|8100
鲜|xian|7400
闲|xian|6800
显|xian|7200
线|xian|7700
限|xian|7600
相|xiang|7700
香|xiang|7800
乡|xiang|7000
想|xiang|8600
向|xiang|7800
象|xiang|6900
像|xiang|7600
笑|xiao|7900
效|xiao|6900
校|xiao|7400
些|xie|7800
写|xie|8200
谢|xie|8000
新|xin|8400
信|xin|8000
星|xing|7300
行|xing|7800
性|xing|7400
姓|xing|6900
幸|xing|7100
兄|xiong|6500
休|xiu|7000
修|xiu|7400
秀|xiu|7000
需|xu|7500
许|xu|7300
须|xu|6800
续|xu|7000
雪|xue|7400
血|xue|7200
寻|xun|6900
训|xun|6800
呀|ya|6600
压|ya|7000
牙|ya|6900
亚|ya|7100
严|yan|7200
言|yan|7300
沿|yan|6700
眼|yan|7800
演|yan|7300
验|yan|7400
阳|yang|7300
洋|yang|7000
养|yang|7200
样|yang|7700
要|yao|8500
药|yao|7600
页|ye|6800
夜|ye|7600
业|ye|7500
医|yi|7400
依|yi|6900
衣|yi|7000
宜|yi|6700
以|yi|7800
椅|yi|6600
意|yi|7700
义|yi|7500
议|yi|7000
易|yi|7200
益|yi|6900
因|yin|7600
音|yin|7700
银|yin|7100
引|yin|7200
饮|yin|6900
印|yin|6800
应|ying|7800
英|ying|7600
营|ying|7000
影|ying|7500
硬|ying|7200
拥|yong|6800
永|yong|7000
优|you|7200
由|you|7500
油|you|7100
游|you|7300
友|you|7600
右|you|7300
又|you|7800
鱼|yu|7500
予|yu|6600
余|yu|7000
雨|yu|7600
语|yu|7700
玉|yu|7100
遇|yu|7400
预|yu|7300
元|yuan|7400
园|yuan|7000
员|yuan|7300
原|yuan|7800
圆|yuan|6900
远|yuan|7700
愿|yuan|7400
院|yuan|7600
月|yue|7800
越|yue|7200
云|yun|7300
运|yun|7600
杂|za|6600
再|zai|7700
赞|zan|6800
脏|zang|6500
早|zao|7900
造|zao|7200
则|ze|6800
责|ze|6900
怎|zen|7000
增|zeng|7300
展|zhan|7400
战|zhan|7500
站|zhan|7800
张|zhang|7700
章|zhang|7000
长|zhang|7600
掌|zhang|6800
招|zhao|7300
照|zhao|7600
者|zhe|7200
着|zhe|7900
真|zhen|7800
镇|zhen|6700
争|zheng|7000
整|zheng|7400
政|zheng|6900
正|zheng|7700
证|zheng|7300
支|zhi|7100
知|zhi|8000
织|zhi|6800
直|zhi|7500
值|zhi|7400
职|zhi|7200
只|zhi|7900
指|zhi|7800
止|zhi|6900
至|zhi|7200
志|zhi|7300
制|zhi|7500
治|zhi|7000
质|zhi|7300
种|zhong|5800
重|zhong|5200
钟|zhong|5000
众|zhong|4800
忠|zhong|4300
终|zhong|4100
仲|zhong|3000
冲|chong|5600
充|chong|5000
重|chong|3500
行|hang|4200
还|hai|4800
了|liao|4600
着|zhao|3900
乐|yue|5400
数|shu|5100
觉|jue|4900
教|jiao|4700
便|pian|4400
差|cha|4300
参|can|4200
藏|cang|4100
藏|zang|3900
朝|chao|4500
朝|zhao|3700
单|dan|4600
单|shan|3800
曾|zeng|4000
曾|ceng|3600
区|qu|5000
区|ou|3400
解|jie|4800
解|xie|3200
薄|bo|4100
薄|bao|3500
和|huo|3700
和|hu|3300
得|dei|3600
州|zhou|7000
周|zhou|7400
洲|zhou|6800
住|zhu|7300
主|zhu|7600
注|zhu|7200
助|zhu|7100
祝|zhu|6800
专|zhuan|7200
转|zhuan|7300
装|zhuang|7300
壮|zhuang|6800
状|zhuang|6900
准|zhun|7400
桌|zhuo|7100
字|zi|8100
总|zong|7700
走|zou|8000
足|zu|7000
组|zu|7500
祖|zu|6900
最|zui|7800
罪|zui|6600
作|zuo|7400
坐|zuo|7600
做|zuo|8000
`

var builtinDictionary = parseDictionary(compactDictionary)

func parseDictionary(data string) []dictionaryEntry {
	lines := strings.Split(data, "\n")
	entries := make([]dictionaryEntry, 0, len(lines))
	seen := make(map[string]int, len(lines))
	for lineNumber, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.Split(line, "|")
		if len(parts) != 3 {
			panic(fmt.Sprintf("ime: invalid dictionary line %d", lineNumber+1))
		}
		text := strings.TrimSpace(parts[0])
		pinyin := strings.ToLower(strings.TrimSpace(parts[1]))
		frequency, err := strconv.Atoi(strings.TrimSpace(parts[2]))
		if text == "" || pinyin == "" || err != nil || frequency <= 0 {
			panic(fmt.Sprintf("ime: invalid dictionary entry on line %d", lineNumber+1))
		}
		syllables := strings.Fields(pinyin)
		if len(syllables) == 0 {
			panic(fmt.Sprintf("ime: missing pinyin on line %d", lineNumber+1))
		}
		for _, syllable := range syllables {
			if !isKeyboardSpelling(syllable) {
				panic(fmt.Sprintf("ime: invalid pinyin on line %d", lineNumber+1))
			}
		}
		key := text + "\x00" + pinyin
		if index, exists := seen[key]; exists {
			// Thematic lists repeat a few common characters. Keep the strongest
			// frequency rather than exposing duplicate candidates.
			if frequency > entries[index].frequency {
				entries[index].frequency = frequency
			}
			continue
		}
		seen[key] = len(entries)
		entries = append(entries, dictionaryEntry{
			text:      text,
			pinyin:    pinyin,
			letters:   strings.Join(syllables, ""),
			syllables: syllables,
			frequency: frequency,
		})
	}
	return entries
}

func isKeyboardSpelling(value string) bool {
	if value == "" {
		return false
	}
	for i := 0; i < len(value); i++ {
		ch := value[i]
		if (ch < 'a' || ch > 'z') && (ch < '0' || ch > '9') {
			return false
		}
	}
	return true
}
