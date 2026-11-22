package desktop

import (
	"encoding/json"
	"fmt"
	"math"
	"math/rand"
	"os"
	"path/filepath"
	"time"

	"win95linux/internal/ui"
)

const (
	bwTile    = 16
	bwW       = 240
	bwH       = 88
	bwD       = 48
	bwMidZ    = bwD / 2
	bwPW      = 12.0
	bwPH      = 26.0
	bwDayLen  = 150.0
	bwSaveDef = "/root/文档/方块世界存档.json"
)

type bwBlock int

const (
	bwAir bwBlock = iota
	bwGrass
	bwDirt
	bwStone
	bwCobble
	bwSand
	bwWood
	bwLeaves
	bwCoal
	bwIron
	bwGold
	bwDiamond
	bwPlanks
	bwGlass
	bwBedrock
	bwCraftingTable
	bwBrick
	bwBlockCount
)

var bwNames = [...]string{
	"空气", "草方块", "泥土", "石头", "圆石", "沙子", "木头", "树叶",
	"煤矿石", "铁矿石", "金矿石", "钻石矿石", "木板", "玻璃", "基岩",
	"工作台", "砖块",
}

var bwHard = [...]float64{
	0, 0.35, 0.3, 1.3, 1.1, 0.3, 0.9, 0.15, 1.7, 1.9, 2.1, 2.5, 0.7, 0.25, -1,
	0.9, 1.4,
}

var bwBaseColor = [...]uint32{
	0, 0x4CA030, 0x7A5533, 0x808080, 0x707070, 0xD8C48A, 0x6B4A2A, 0x2E7A2E,
	0x808080, 0x808080, 0x808080, 0x808080, 0xB08050, 0x9FD8F0, 0x4A4A4A,
	0x9C7040, 0xA84838,
}

func bwIsSolid(b bwBlock) bool { return b != bwAir }

type bwSlot struct {
	Block bwBlock `json:"block"`
	Count int     `json:"count"`
}

type bwParticle struct {
	x, y, vx, vy, life float64
	col                uint32
	size               int
}

type bwWorld struct {
	W, D, H int
	Seed    int64
	Tiles   []bwBlock
	Heights []int // surface height at the middle depth, used by the 2D slice view
}

func (w *bwWorld) index(x, y, z int) int {
	return (y*w.D+z)*w.W + x
}

func (w *bwWorld) At3(x, y, z int) bwBlock {
	if x < 0 || x >= w.W || y < 0 || y >= w.H || z < 0 || z >= w.D {
		return bwAir
	}
	return w.Tiles[w.index(x, y, z)]
}

func (w *bwWorld) Set3(x, y, z int, b bwBlock) {
	if x < 0 || x >= w.W || y < 0 || y >= w.H || z < 0 || z >= w.D {
		return
	}
	w.Tiles[w.index(x, y, z)] = b
}

func (w *bwWorld) At(x, y int) bwBlock {
	return w.At3(x, y, bwMidZ)
}

func (w *bwWorld) Set(x, y int, b bwBlock) {
	w.Set3(x, y, bwMidZ, b)
}

func (w *bwWorld) columnTop3(x, z int) int {
	if x < 0 || x >= w.W || z < 0 || z >= w.D {
		return w.H - 1
	}
	for y := 0; y < w.H; y++ {
		if w.At3(x, y, z) != bwAir {
			return y
		}
	}
	return w.H - 1
}

func (w *bwWorld) columnTop(x int) int {
	return w.columnTop3(x, bwMidZ)
}

// newBWWorld builds a real volumetric world: heightmap terrain for every
// (x,z) column, 3D cave worms, ore veins and trees. At()/Set() address the
// middle slice so the classic side view remains a valid cross-section.
func newBWWorld(seed int64) *bwWorld {
	w := &bwWorld{W: bwW, D: bwD, H: bwH, Seed: seed,
		Tiles: make([]bwBlock, bwW*bwD*bwH), Heights: make([]int, bwW)}
	rng := rand.New(rand.NewSource(seed))
	heights := make([]int, bwW*bwD)
	ph1 := float64(seed%89) / 7
	ph2 := float64(seed%97) / 11
	ph3 := float64(seed%53) / 5
	for z := 0; z < w.D; z++ {
		for x := 0; x < w.W; x++ {
			fx, fz := float64(x), float64(z)
			base := 46.0 +
				10*math.Sin(fx/60+ph1) +
				6*math.Sin(fx/23+ph2) +
				5*math.Sin(fz/30+ph3) +
				2.5*math.Sin((fx+fz)/14+ph1)
			h := int(math.Round(base))
			if h < 24 {
				h = 24
			}
			if h > w.H-8 {
				h = w.H - 8
			}
			heights[z*w.W+x] = h
			if z == bwMidZ {
				w.Heights[x] = h
			}
		}
	}
	for z := 0; z < w.D; z++ {
		for x := 0; x < w.W; x++ {
			h := heights[z*w.W+x]
			sand := h >= 52 && math.Sin(float64(x)/19+ph3)*math.Sin(float64(z)/23+ph1) > 0.5
			for y := h; y < w.H; y++ {
				var b bwBlock
				switch {
				case y == w.H-1:
					b = bwBedrock
				case y >= w.H-2 && bwHash(uint32(x*7+z), uint32(y)^uint32(seed))%2 == 0:
					b = bwBedrock
				case y == h && sand:
					b = bwSand
				case y <= h+3 && sand:
					b = bwSand
				case y == h:
					b = bwGrass
				case y <= h+3:
					b = bwDirt
				default:
					b = bwStone
				}
				w.Set3(x, y, z, b)
			}
		}
	}
	// Cave worms wander through all three axes, so caves are real cavities
	// rather than vertical stripes in a cross-section.
	for i := 0; i < 22; i++ {
		cx := 4 + rng.Intn(w.W-8)
		cz := 4 + rng.Intn(w.D-8)
		cy := heights[cz*w.W+cx] + 8 + rng.Intn(18)
		steps := 45 + rng.Intn(70)
		for s := 0; s < steps; s++ {
			r := 1
			if rng.Intn(3) == 0 {
				r = 2
			}
			for dy := -r; dy <= r; dy++ {
				for dz := -r; dz <= r; dz++ {
					for dx := -r; dx <= r; dx++ {
						if dx*dx+dy*dy+dz*dz > r*r {
							continue
						}
						x, y, z := cx+dx, cy+dy, cz+dz
						if x < 1 || x >= w.W-1 || z < 1 || z >= w.D-1 || y < 3 || y >= w.H-3 {
							continue
						}
						if y <= heights[z*w.W+x]+4 {
							continue
						}
						if w.At3(x, y, z) != bwBedrock {
							w.Set3(x, y, z, bwAir)
						}
					}
				}
			}
			cx += rng.Intn(3) - 1
			cy += rng.Intn(3) - 1
			cz += rng.Intn(3) - 1
			cx = min(max(cx, 1), w.W-2)
			cz = min(max(cz, 1), w.D-2)
			cy = min(max(cy, 4), w.H-5)
		}
	}
	// Ore veins.
	for i := 0; i < w.W*w.D*w.H/1100; i++ {
		x := 1 + rng.Intn(w.W-2)
		z := 1 + rng.Intn(w.D-2)
		top := heights[z*w.W+x] + 5
		if top >= w.H-4 {
			continue
		}
		y := top + rng.Intn(w.H-3-top)
		depth := y - heights[z*w.W+x]
		roll := rng.Intn(1000)
		var ore bwBlock
		switch {
		case depth > 30 && roll < 24:
			ore = bwDiamond
		case depth > 20 && roll < 76:
			ore = bwGold
		case depth > 10 && roll < 190:
			ore = bwIron
		case roll < 440:
			ore = bwCoal
		default:
			continue
		}
		for dy := 0; dy < 2; dy++ {
			for dz := 0; dz < 2; dz++ {
				for dx := 0; dx < 2; dx++ {
					if w.At3(x+dx, y+dy, z+dz) == bwStone {
						w.Set3(x+dx, y+dy, z+dz, ore)
					}
				}
			}
		}
	}
	plantTree := func(x, z int) {
		h := heights[z*w.W+x]
		if h < 12 {
			return
		}
		trunk := 4 + int(bwHash(uint32(x), uint32(z))%3)
		if h-trunk < 3 {
			return
		}
		for i := 1; i <= trunk; i++ {
			w.Set3(x, h-i, z, bwWood)
		}
		top := h - trunk
		for dy := -2; dy <= 1; dy++ {
			radius := 2
			if dy == -2 {
				radius = 1
			}
			for dz := -radius; dz <= radius; dz++ {
				for dx := -radius; dx <= radius; dx++ {
					if dx == 0 && dz == 0 && dy <= 0 {
						continue
					}
					nx, ny, nz := x+dx, top+dy, z+dz
					if nx < 0 || nx >= w.W || nz < 0 || nz >= w.D || ny < 1 {
						continue
					}
					if w.At3(nx, ny, nz) == bwAir {
						w.Set3(nx, ny, nz, bwLeaves)
					}
				}
			}
		}
		if w.At3(x, top-1, z) == bwAir {
			w.Set3(x, top-1, z, bwLeaves)
		}
	}
	for z := 5; z < w.D-5; z += 4 + int(bwHash(uint32(z), uint32(seed))%5) {
		for x := 7; x < w.W-7; x += 8 + int(bwHash(uint32(x), uint32(z)^uint32(seed))%9) {
			if bwHash(uint32(x+z*131), uint32(seed))%4 != 0 {
				continue
			}
			h := heights[z*w.W+x]
			if w.At3(x, h, z) != bwGrass {
				continue
			}
			plantTree(x, z)
		}
	}
	// Guarantee that the middle cross-section contains wood, leaves and coal
	// so both the classic view and the 3D spawn area have recognizable props.
	for _, x := range []int{21, 67, 122, 181, 219} {
		if x >= w.W {
			continue
		}
		h := heights[bwMidZ*w.W+x]
		w.Set3(x, h, bwMidZ, bwGrass)
		plantTree(x, bwMidZ)
	}
	cx := 40
	ch := heights[bwMidZ*w.W+cx]
	for dy := 5; dy < 10; dy++ {
		for dz := bwMidZ - 1; dz <= bwMidZ+1; dz++ {
			if w.At3(cx, ch+dy, dz) == bwStone {
				w.Set3(cx, ch+dy, dz, bwCoal)
			}
		}
	}
	return w
}

func (w *bwWorld) rle() []byte {
	out := make([]byte, 0, len(w.Tiles)/3)
	var cur bwBlock = -1
	count := 0
	flush := func() {
		if count > 0 {
			out = append(out, byte(cur), byte(count))
		}
	}
	for _, b := range w.Tiles {
		if b == cur && count < 255 {
			count++
			continue
		}
		flush()
		cur = b
		count = 1
	}
	flush()
	return out
}

func bwDecodeRLE(data []byte, n int) ([]bwBlock, error) {
	if len(data)%2 != 0 {
		return nil, fmt.Errorf("存档数据不完整")
	}
	tiles := make([]bwBlock, 0, n)
	for i := 0; i < len(data); i += 2 {
		b := bwBlock(data[i])
		c := int(data[i+1])
		if c == 0 || b >= bwBlockCount {
			return nil, fmt.Errorf("存档包含无效方块")
		}
		if len(tiles)+c > n {
			return nil, fmt.Errorf("存档尺寸与方块数量不匹配")
		}
		for j := 0; j < c; j++ {
			tiles = append(tiles, b)
		}
	}
	if len(tiles) != n {
		return nil, fmt.Errorf("存档方块数量不足")
	}
	return tiles, nil
}

type bwKeyState struct {
	left, right bool
	forward     bool
	backward    bool
	lookLeft    bool
	lookRight   bool
	lookUp      bool
	lookDown    bool
	until       time.Time
}

type bwSaveFile struct {
	Version int      `json:"version"`
	Seed    int64    `json:"seed"`
	W       int      `json:"w"`
	D       int      `json:"d,omitempty"`
	H       int      `json:"h"`
	Heights []int    `json:"heights"`
	Tiles   []byte   `json:"tiles"`
	X       float64  `json:"x"`
	Y       float64  `json:"y"`
	Z       float64  `json:"z,omitempty"`
	Yaw     float64  `json:"yaw,omitempty"`
	Pitch   float64  `json:"pitch,omitempty"`
	Hotbar  []bwSlot `json:"hotbar"`
	Sel     int      `json:"sel"`
	Phase   float64  `json:"phase"`
}

type BlockWorldApp struct {
	Base
	d   *Desktop
	win *Window

	world *bwWorld
	seed  int64
	phase float64

	px, py, pz float64
	vx, vy, vz float64
	face       int
	onGround   bool
	walk       float64

	// First-person camera. yaw rotates around the vertical axis, pitch looks
	// up (negative) or down (positive); both are radians.
	yaw, pitch float64
	viewDist   float64

	hover3Valid  bool
	hover3X      int
	hover3Y      int
	hover3Z      int
	hover3PX     int
	hover3PY     int
	hover3PZ     int
	mine3X       int
	mine3Y       int
	mine3Z       int
	lastMouseX   int
	lastMouseY   int
	mouseLooking bool

	hotbar [9]bwSlot
	sel    int

	mining     bool
	mineProg   float64
	mineTx     int
	mineTy     int
	hoverX     int
	hoverY     int
	hoverValid bool

	particles []bwParticle
	rng       *rand.Rand

	keys     bwKeyState
	lastJump time.Time

	status      string
	statusUntil time.Time
	dirty       bool
	savePath    string

	client      ui.Rect
	camX, camY  int
	lastAnim    time.Time
	animStarted bool
	cloudShift  float64

	craftOpen bool
	craftSel  int
	view3D    bool
	floats    []bwFloatText
}

func NewBlockWorldApp(d *Desktop) *BlockWorldApp {
	a := &BlockWorldApp{
		d:        d,
		rng:      rand.New(rand.NewSource(time.Now().UnixNano())),
		savePath: bwSaveDef,
		view3D:   true,
		viewDist: 96.0,
	}
	a.newWorld(time.Now().UnixNano())
	return a
}

func (a *BlockWorldApp) Title() string           { return "方块世界 95" }
func (a *BlockWorldApp) Icon() ui.IconKind       { return ui.IcoGame }
func (a *BlockWorldApp) DefaultSize() (int, int) { return 960, 640 }
func (a *BlockWorldApp) MinSize() (int, int)     { return 640, 480 }
func (a *BlockWorldApp) Resizable() bool         { return true }

func (a *BlockWorldApp) Menus(d *Desktop, w *Window) []Menu {
	return []Menu{
		{Label: "游戏(G)", Items: []MenuItem{
			{Label: "新世界(N)", Accel: "F2", Enabled: true, Action: func() { a.confirmNewWorld(d) }},
			{Label: "保存世界(S)", Accel: "Ctrl+S", Enabled: true, Action: func() { a.saveWorld(true) }},
			{Label: "读取世界(L)", Accel: "Ctrl+O", Enabled: true, Action: func() { a.loadWorld(true) }},
			{Separator: true},
			{Label: "合成面板(E)", Accel: "E", Enabled: true, Action: func() { a.toggleCraft(d) }},
			{Label: "3D 立体视角(V)", Accel: "V", Enabled: true, Action: func() { a.toggle3D(d) }},
			{Separator: true},
			{Label: "退出(X)", Enabled: true, Action: func() { d.CloseWindow(w) }},
		}},
		{Label: "帮助(H)", Items: []MenuItem{
			{Label: "操作说明(O)", Enabled: true, Action: func() { a.showHelp(d) }},
			{Label: "关于方块世界(A)", Enabled: true, Action: func() { a.showAbout(d) }},
		}},
	}
}

func (a *BlockWorldApp) newWorld(seed int64) {
	a.seed = seed
	a.world = newBWWorld(seed)
	a.phase = 0.30
	a.hotbar = [9]bwSlot{
		{bwGrass, 12}, {bwDirt, 32}, {bwStone, 20}, {bwPlanks, 15}, {bwGlass, 10},
		{bwWood, 10}, {bwSand, 10}, {bwLeaves, 10}, {bwCobble, 10},
	}
	a.sel = 0
	a.particles = nil
	a.floats = nil
	a.craftOpen = false
	a.craftSel = 0
	a.mining = false
	a.mineProg = 0
	a.dirty = false
	a.status = ""
	a.spawn()
}

func (a *BlockWorldApp) spawn() {
	mid := a.world.W / 2
	bestX, bestY, bestYaw, bestScore := -1, -1, 0.0, -1
	for x := 8; x < a.world.W-8; x += 2 {
		y := a.world.columnTop(x)
		if y < 3 || y >= a.world.H-6 {
			continue
		}
		b := a.world.At(x, y)
		if b != bwGrass && b != bwSand {
			continue
		}
		if a.world.At(x, y-1) != bwAir || a.world.At(x, y-2) != bwAir {
			continue
		}
		if !a.spawnClear(x, y) {
			continue
		}
		yaw, score := a.openView(x, y)
		if score > bestScore {
			bestX, bestY, bestYaw, bestScore = x, y, yaw, score
		}
	}
	if bestX < 0 {
		bestX, bestY = mid, a.world.Heights[mid]
		bestYaw = a.openYaw(bestX, bestY)
	}
	a.px = float64(bestX*bwTile + 2)
	a.py = float64(bestY*bwTile) - bwPH
	a.pz = float64(bwMidZ*bwTile) + (bwTile-bwPW)/2
	a.vx, a.vy, a.vz = 0, 0, 0
	a.onGround = true
	a.face = 1
	a.yaw, a.pitch = bestYaw, 0
}

// spawnClear keeps the first-person spawn out of tree trunks and canopies so
// the view opens onto the landscape instead of the inside of a leaf block.
func (a *BlockWorldApp) spawnClear(x, y int) bool {
	for dz := -2; dz <= 2; dz++ {
		for dx := -2; dx <= 2; dx++ {
			for dy := -5; dy <= 0; dy++ {
				switch a.world.At3(x+dx, y+dy, bwMidZ+dz) {
				case bwWood, bwLeaves:
					return false
				}
			}
		}
	}
	return true
}

func (a *BlockWorldApp) showHelp(d *Desktop) {
	d.MessageBox("方块世界 95 - 操作说明",
		"移动：A / D 或 ← / →\n"+
			"3D 移动：W A S D　跳跃：空格\n"+
			"视角：方向键，或按住右键拖动\n"+
			"挖掘：按住鼠标左键　放置：鼠标右键\n"+
			"选择方块：1-9 或滚轮\n"+
			"合成面板：E　3D/剖面切换：V\n"+
			"保存世界：Ctrl+S　读取：Ctrl+O\n\n"+
			"提示：基岩无法破坏；放置方块时需要相邻方块支撑，\n"+
			"而且不能放在自己身上。按 E 打开合成面板，把木头合成\n"+
			"木板、再做工作台；高级配方需要站在放置的工作台旁边。\n"+
			"世界会自动保存在 /root/文档/方块世界存档.json。\n"+
			"挖掘、放置、跳跃与合成有音效（依赖主板蜂鸣器，UTM 可能无声）。",
		IconInfo, []string{"确定"}, nil)
}

func (a *BlockWorldApp) showAbout(d *Desktop) {
	d.MessageBox("关于方块世界 95",
		"方块世界 95\n版本 26.5\n\n"+
			"一个在 Windows 95 Linux 桌面上运行的\n三维体素沙盒小游戏：挖矿、建造、看日出。\n\n"+
			"3D 视角由自研体素光栅化引擎逐像素光线步进渲染，\n"+
			"具有真实的体积遮挡、透视和昼夜光照。\n"+
			"目标环境 UTM Apple 虚拟化不提供 GPU，因此画面由 CPU\n"+
			"实时渲染；Xorg 下的 Firefox 等程序可使用 Mesa 软件 GL。\n\n"+
			"本游戏只写入自己的存档文件，\n不会修改系统设置，也不会接触磁盘分区。",
		IconInfo, []string{"确定"}, nil)
}

func (a *BlockWorldApp) confirmNewWorld(d *Desktop) {
	d.MessageBox("方块世界 95", "生成新世界会丢弃当前未保存的修改。\n\n继续吗？", IconQuestion, []string{"是", "否"}, func(i int) {
		if i == 0 {
			a.newWorld(time.Now().UnixNano())
			a.setStatus("新世界已生成")
			d.Invalidate()
		}
	})
}

func (a *BlockWorldApp) setStatus(s string) {
	a.status = s
	a.statusUntil = time.Now().Add(3200 * time.Millisecond)
}

func (a *BlockWorldApp) saveWorld(notify bool) {
	if err := a.writeSave(); err != nil {
		a.setStatus("保存失败：" + err.Error())
		if notify && a.d != nil {
			a.d.MessageBox("方块世界 95", "无法保存世界：\n"+err.Error(), IconError, []string{"确定"}, nil)
		}
		return
	}
	a.dirty = false
	if notify {
		a.setStatus("世界已保存：" + a.savePath)
		if a.d != nil {
			a.d.Notify("方块世界 95", "世界已保存到 "+a.savePath)
		}
	}
}

func (a *BlockWorldApp) writeSave() error {
	if a.world == nil {
		return fmt.Errorf("没有可保存的世界")
	}
	path := a.savePath
	if path == "" {
		path = bwSaveDef
	}
	sv := bwSaveFile{
		Version: 3,
		Seed:    a.seed,
		W:       a.world.W,
		D:       a.world.D,
		H:       a.world.H,
		Heights: a.world.Heights,
		Tiles:   a.world.rle(),
		X:       a.px,
		Y:       a.py,
		Z:       a.pz,
		Yaw:     a.yaw,
		Pitch:   a.pitch,
		Hotbar:  a.hotbar[:],
		Sel:     a.sel,
		Phase:   a.phase,
	}
	data, err := json.MarshalIndent(sv, "", " ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func (a *BlockWorldApp) loadWorld(notify bool) {
	path := a.savePath
	if path == "" {
		path = bwSaveDef
	}
	data, err := os.ReadFile(path)
	if err != nil {
		a.setStatus("没有找到存档")
		if notify && a.d != nil {
			a.d.MessageBox("方块世界 95", "无法读取存档：\n"+err.Error(), IconWarning, []string{"确定"}, nil)
		}
		return
	}
	var sv bwSaveFile
	if err := json.Unmarshal(data, &sv); err != nil {
		a.setStatus("存档已损坏")
		if notify && a.d != nil {
			a.d.MessageBox("方块世界 95", "存档已损坏：\n"+err.Error(), IconError, []string{"确定"}, nil)
		}
		return
	}
	if err := a.applySave(sv); err != nil {
		a.setStatus("存档无效：" + err.Error())
		if notify && a.d != nil {
			a.d.MessageBox("方块世界 95", "存档无效：\n"+err.Error(), IconError, []string{"确定"}, nil)
		}
		return
	}
	a.setStatus("世界已读取")
	if notify && a.d != nil {
		a.d.Notify("方块世界 95", "世界已读取")
	}
}

func (a *BlockWorldApp) applySave(sv bwSaveFile) error {
	if sv.W != bwW || sv.H != bwH || sv.D < 0 || sv.D > 512 {
		return fmt.Errorf("世界尺寸无效")
	}
	legacy2D := sv.D == 0
	volume := sv.W * sv.H
	if !legacy2D {
		volume = sv.W * sv.D * sv.H
	}
	tiles, err := bwDecodeRLE(sv.Tiles, volume)
	if err != nil {
		return err
	}
	if len(sv.Heights) != sv.W {
		return fmt.Errorf("地形高度数据不完整")
	}
	var w *bwWorld
	if legacy2D {
		// Version 2 saves only stored the middle cross-section. Expand them
		// into a fresh 3D world instead of rejecting an old save.
		w = newBWWorld(sv.Seed)
		for y := 0; y < sv.H; y++ {
			for x := 0; x < sv.W; x++ {
				w.Set3(x, y, bwMidZ, tiles[y*sv.W+x])
			}
		}
		w.Heights = append([]int(nil), sv.Heights...)
	} else if sv.D == bwD {
		w = &bwWorld{W: sv.W, D: sv.D, H: sv.H, Seed: sv.Seed, Tiles: tiles,
			Heights: append([]int(nil), sv.Heights...)}
	} else {
		return fmt.Errorf("存档深度不受支持")
	}
	a.world = w
	a.seed = sv.Seed
	if len(sv.Hotbar) == 9 {
		for i, s := range sv.Hotbar {
			if s.Block < bwAir || s.Block >= bwBlockCount || s.Count < 0 {
				a.hotbar[i] = bwSlot{}
				continue
			}
			if s.Count > 99 {
				s.Count = 99
			}
			a.hotbar[i] = s
		}
	}
	if sv.Sel >= 0 && sv.Sel < 9 {
		a.sel = sv.Sel
	}
	a.phase = math.Mod(math.Abs(sv.Phase), 1)
	a.px, a.py = sv.X, sv.Y
	a.pz = sv.Z
	if a.pz <= 0 {
		a.pz = float64(bwMidZ*bwTile) + (bwTile-bwPW)/2
	}
	a.yaw, a.pitch = sv.Yaw, sv.Pitch
	if math.Abs(a.pitch) > 1.45 {
		a.pitch = 0
	}
	a.vx, a.vy, a.vz = 0, 0, 0
	if a.px < 0 || a.py < 0 || a.px > float64(w.W*bwTile) || a.py > float64(w.H*bwTile) ||
		a.pz < 0 || a.pz > float64(w.D*bwTile) {
		a.spawn()
	}
	a.particles = nil
	a.floats = nil
	a.mining = false
	a.mineProg = 0
	a.hover3Valid = false
	a.mouseLooking = false
	a.dirty = false
	return nil
}

func (a *BlockWorldApp) Close() {
	if a.dirty && a.world != nil {
		_ = a.writeSave()
	}
}

func (a *BlockWorldApp) Resized(d *Desktop, w *Window, client ui.Rect) {
	a.client = client
	a.updateCamera(client)
}

func (a *BlockWorldApp) Animate(d *Desktop, w *Window, now time.Time) {
	if !a.animStarted || a.lastAnim.IsZero() {
		a.animStarted = true
		a.lastAnim = now
		return
	}
	dt := now.Sub(a.lastAnim).Seconds()
	a.lastAnim = now
	if dt <= 0 {
		return
	}
	if dt > 0.08 {
		dt = 0.08
	}
	a.phase = math.Mod(a.phase+dt/bwDayLen, 1)
	a.cloudShift += dt * 5
	a.stepKeys(now)
	if a.view3D {
		if a.keys.lookLeft {
			a.yaw -= dt * 1.9
		}
		if a.keys.lookRight {
			a.yaw += dt * 1.9
		}
		if a.keys.lookUp {
			a.pitch -= dt * 1.4
		}
		if a.keys.lookDown {
			a.pitch += dt * 1.4
		}
		a.pitch = math.Max(-1.45, math.Min(1.45, a.pitch))
		a.stepPhysics3(dt)
		a.updateHover3()
		a.stepMining3(dt)
	} else {
		a.stepPhysics(dt)
		a.stepMining(dt)
	}
	a.stepParticles(dt)
	a.stepFloats(dt)
	d.Invalidate()
}

func (a *BlockWorldApp) stepKeys(now time.Time) {
	if now.After(a.keys.until) {
		a.keys = bwKeyState{}
	}
}

func (a *BlockWorldApp) stepPhysics(dt float64) {
	dir := 0
	if a.keys.left {
		dir--
	}
	if a.keys.right {
		dir++
	}
	if dir != 0 {
		a.face = dir
		a.walk += dt * 11
	} else {
		a.walk = 0
	}
	a.vx = float64(dir) * 108
	a.vy += 900 * dt
	if a.vy > 640 {
		a.vy = 640
	}
	if a.vx != 0 {
		a.moveHorizontal(dt)
	}
	a.moveVertical(dt)
	if a.py > float64(a.world.H*bwTile)+240 {
		a.spawn()
		a.setStatus("你掉出了世界，已回到出生点")
	}
}

func (a *BlockWorldApp) moveHorizontal(dt float64) {
	prevX := a.px
	a.px += a.vx * dt
	if !a.collidesAt(a.px, a.py) {
		return
	}
	if a.onGround {
		oldY := a.py
		a.py -= bwTile
		if !a.collidesAt(a.px, a.py) {
			return
		}
		a.py = oldY
	}
	a.px = prevX
	a.vx = 0
}

func (a *BlockWorldApp) moveVertical(dt float64) {
	a.py += a.vy * dt
	a.onGround = false
	if a.vy > 0 {
		minTop, _, any := a.solidBounds()
		if any {
			a.py = float64(minTop) - bwPH
			a.vy = 0
			a.onGround = true
			if a.collidesAt(a.px, a.py) {
				a.py -= 1
			}
		}
	} else if a.vy < 0 {
		_, maxBot, any := a.solidBounds()
		if any {
			a.py = float64(maxBot)
			a.vy = 0
			if a.collidesAt(a.px, a.py) {
				a.py += 1
			}
		}
	}
}

func (a *BlockWorldApp) solidBounds() (int, int, bool) {
	x0 := int(math.Floor(a.px / bwTile))
	x1 := int(math.Floor((a.px + bwPW - 0.001) / bwTile))
	y0 := int(math.Floor(a.py / bwTile))
	y1 := int(math.Floor((a.py + bwPH - 0.001) / bwTile))
	minTop := 1 << 30
	maxBot := -1 << 30
	any := false
	for ty := y0; ty <= y1; ty++ {
		for tx := x0; tx <= x1; tx++ {
			if !a.solidTile(tx, ty) {
				continue
			}
			any = true
			if top := ty * bwTile; top < minTop {
				minTop = top
			}
			if bot := (ty + 1) * bwTile; bot > maxBot {
				maxBot = bot
			}
		}
	}
	return minTop, maxBot, any
}

func (a *BlockWorldApp) collidesAt(x, y float64) bool {
	x0 := int(math.Floor(x / bwTile))
	x1 := int(math.Floor((x + bwPW - 0.001) / bwTile))
	y0 := int(math.Floor(y / bwTile))
	y1 := int(math.Floor((y + bwPH - 0.001) / bwTile))
	for ty := y0; ty <= y1; ty++ {
		for tx := x0; tx <= x1; tx++ {
			if a.solidTile(tx, ty) {
				return true
			}
		}
	}
	return false
}

func (a *BlockWorldApp) solidTile(tx, ty int) bool {
	if tx < 0 || tx >= a.world.W {
		return true
	}
	if ty < 0 {
		return false
	}
	if ty >= a.world.H {
		return true
	}
	return bwIsSolid(a.world.At(tx, ty))
}

func (a *BlockWorldApp) withinReach(tx, ty int) bool {
	if tx < 0 || tx >= a.world.W || ty < 0 || ty >= a.world.H {
		return false
	}
	cx := a.px + bwPW/2
	cy := a.py + bwPH/2
	dx := float64(tx*bwTile+bwTile/2) - cx
	dy := float64(ty*bwTile+bwTile/2) - cy
	const reach = 7 * bwTile
	return dx*dx+dy*dy <= reach*reach
}

func (a *BlockWorldApp) stepMining(dt float64) {
	if !a.mining || !a.hoverValid {
		a.mineProg = 0
		return
	}
	tx, ty := a.hoverX, a.hoverY
	if tx != a.mineTx || ty != a.mineTy {
		a.mineTx, a.mineTy = tx, ty
		a.mineProg = 0
	}
	if !a.withinReach(tx, ty) {
		a.mineProg = 0
		return
	}
	b := a.world.At(tx, ty)
	hard := 0.0
	if int(b) >= 0 && int(b) < len(bwHard) {
		hard = bwHard[b]
	}
	if b == bwAir || hard < 0 {
		a.mineProg = 0
		return
	}
	a.mineProg += dt
	if a.mineProg >= hard {
		a.mineProg = 0
		a.breakBlock(tx, ty)
	}
}

func (a *BlockWorldApp) breakBlock(tx, ty int) bool {
	b := a.world.At(tx, ty)
	if b == bwAir || b == bwBedrock {
		return false
	}
	a.world.Set(tx, ty, bwAir)
	a.dirty = true
	drop := b
	if b == bwGrass {
		drop = bwDirt
	}
	a.addItem(drop)
	a.spawnParticles(tx, ty, b, 8)
	a.addFloat(tx, ty, "+1 "+bwNames[drop], 0xF0F0A0)
	bwSound("dig")
	if a.d != nil {
		a.d.Invalidate()
	}
	return true
}

func (a *BlockWorldApp) addItem(b bwBlock) {
	for i := range a.hotbar {
		if a.hotbar[i].Block == b && a.hotbar[i].Count > 0 && a.hotbar[i].Count < 99 {
			a.hotbar[i].Count++
			return
		}
	}
	for i := range a.hotbar {
		if a.hotbar[i].Count == 0 {
			a.hotbar[i] = bwSlot{Block: b, Count: 1}
			return
		}
	}
	a.setStatus("背包已满，无法拾取" + bwNames[b])
}

func (a *BlockWorldApp) placeBlock(tx, ty int) {
	if tx < 0 || tx >= a.world.W || ty < 1 || ty >= a.world.H-1 {
		return
	}
	if !a.withinReach(tx, ty) {
		a.setStatus("距离太远，无法放置")
		return
	}
	if a.world.At(tx, ty) != bwAir {
		return
	}
	slot := &a.hotbar[a.sel]
	if slot.Count <= 0 {
		a.setStatus("这个方块已经用完了")
		return
	}
	adjacent := a.world.At(tx-1, ty) != bwAir || a.world.At(tx+1, ty) != bwAir ||
		a.world.At(tx, ty-1) != bwAir || a.world.At(tx, ty+1) != bwAir
	if !adjacent {
		a.setStatus("方块需要相邻支撑")
		return
	}
	if a.blockHitsPlayer(tx, ty) {
		a.setStatus("不能把方块放在自己身上")
		return
	}
	a.world.Set(tx, ty, slot.Block)
	slot.Count--
	a.dirty = true
	a.spawnParticles(tx, ty, slot.Block, 4)
	bwSound("place")
	if a.d != nil {
		a.d.Invalidate()
	}
}

func (a *BlockWorldApp) blockHitsPlayer(tx, ty int) bool {
	bx := float64(tx * bwTile)
	by := float64(ty * bwTile)
	return bx < a.px+bwPW && bx+bwTile > a.px && by < a.py+bwPH && by+bwTile > a.py
}

func (a *BlockWorldApp) spawnParticles(tx, ty int, b bwBlock, n int) {
	base := bwBaseColor[b]
	for i := 0; i < n; i++ {
		a.particles = append(a.particles, bwParticle{
			x:    float64(tx*bwTile) + a.rng.Float64()*bwTile,
			y:    float64(ty*bwTile) + a.rng.Float64()*bwTile,
			vx:   (a.rng.Float64() - 0.5) * 60,
			vy:   -a.rng.Float64() * 70,
			life: 0.45 + a.rng.Float64()*0.4,
			col:  bwShade(base, 0.7+a.rng.Float64()*0.5),
			size: 1 + a.rng.Intn(2),
		})
	}
	if len(a.particles) > 240 {
		a.particles = a.particles[len(a.particles)-240:]
	}
}

func (a *BlockWorldApp) stepParticles(dt float64) {
	out := a.particles[:0]
	for _, p := range a.particles {
		p.life -= dt
		if p.life <= 0 {
			continue
		}
		p.vy += 320 * dt
		p.x += p.vx * dt
		p.y += p.vy * dt
		out = append(out, p)
	}
	a.particles = out
}

func (a *BlockWorldApp) jump() {
	if !a.onGround {
		return
	}
	if time.Since(a.lastJump) < 240*time.Millisecond {
		return
	}
	a.lastJump = time.Now()
	a.vy = -310
	a.onGround = false
	bwSound("jump")
}

func (a *BlockWorldApp) Key(d *Desktop, w *Window, ev KeyEvent) bool {
	if a.craftOpen {
		return a.craftKey(d, ev)
	}
	if ev.Ctrl && ev.Name == "S" {
		a.saveWorld(true)
		return true
	}
	if ev.Ctrl && ev.Name == "O" {
		a.loadWorld(true)
		return true
	}
	switch ev.Name {
	case "F2":
		a.confirmNewWorld(d)
		return true
	case "E":
		a.toggleCraft(d)
		return true
	case "V":
		a.toggle3D(d)
		return true
	}
	if a.view3D {
		switch ev.Name {
		case "A":
			a.keys.left = true
		case "D":
			a.keys.right = true
		case "W":
			a.keys.forward = true
		case "S":
			a.keys.backward = true
		case "Left":
			a.keys.lookLeft = true
		case "Right":
			a.keys.lookRight = true
		case "Up":
			a.keys.lookUp = true
		case "Down":
			a.keys.lookDown = true
		case " ":
			a.jump()
		default:
			if ev.Rune >= '1' && ev.Rune <= '9' {
				a.sel = int(ev.Rune - '1')
				return true
			}
			return false
		}
		a.keys.until = time.Now().Add(240 * time.Millisecond)
		return true
	}
	switch ev.Name {
	case "A", "Left":
		a.keys.left = true
		a.keys.until = time.Now().Add(240 * time.Millisecond)
		return true
	case "D", "Right":
		a.keys.right = true
		a.keys.until = time.Now().Add(240 * time.Millisecond)
		return true
	case "W", "Up", " ":
		a.jump()
		return true
	}
	if ev.Rune >= '1' && ev.Rune <= '9' {
		a.sel = int(ev.Rune - '1')
		return true
	}
	return false
}

func (a *BlockWorldApp) Mouse(d *Desktop, w *Window, client ui.Rect, ev MouseEvent) {
	a.client = client
	if a.craftOpen {
		a.craftMouse(d, client, ev)
		return
	}
	if ev.Kind == MouseWheel {
		if ev.Wheel != 0 {
			a.sel = (a.sel - ev.Wheel%9 + 9) % 9
			d.Invalidate()
		}
		return
	}
	if a.view3D {
		a.mouse3D(d, client, ev)
		return
	}
	a.updateCamera(client)
	tx, ty, ok := a.tileAt(client, ev.X, ev.Y)
	if ev.Kind == MouseMove {
		a.hoverX, a.hoverY, a.hoverValid = tx, ty, ok
		return
	}
	switch ev.Button {
	case 1:
		if ev.Kind == MouseDown {
			for i := 0; i < 9; i++ {
				if hotbarRect(client, i).Contains(ev.X, ev.Y) {
					a.sel = i
					a.mining = false
					a.mineProg = 0
					d.Invalidate()
					return
				}
			}
			if ok && a.withinReach(tx, ty) {
				b := a.world.At(tx, ty)
				if b == bwBedrock {
					a.setStatus("基岩非常坚硬，无法破坏")
					return
				}
				if b == bwAir {
					return
				}
				a.mining = true
				a.mineProg = 0
				a.mineTx, a.mineTy = tx, ty
				a.hoverX, a.hoverY, a.hoverValid = tx, ty, true
			}
		} else if ev.Kind == MouseUp {
			a.mining = false
			a.mineProg = 0
			d.Invalidate()
		}
	case 3:
		if ev.Kind == MouseDown {
			a.hoverX, a.hoverY, a.hoverValid = tx, ty, ok
			if ok {
				a.placeBlock(tx, ty)
			}
			d.Invalidate()
		}
	}
}

func (a *BlockWorldApp) mouse3D(d *Desktop, client ui.Rect, ev MouseEvent) {
	for i := 0; i < 9; i++ {
		if hotbarRect(client, i).Contains(ev.X, ev.Y) && ev.Kind == MouseDown {
			a.sel = i
			a.mining = false
			a.mineProg = 0
			d.Invalidate()
			return
		}
	}
	if ev.Kind == MouseMove {
		dx := ev.X - a.lastMouseX
		dy := ev.Y - a.lastMouseY
		a.lastMouseX, a.lastMouseY = ev.X, ev.Y
		if a.mouseLooking {
			a.yaw += float64(dx) * 0.006
			a.pitch = math.Max(-1.45, math.Min(1.45, a.pitch+float64(dy)*0.006))
			a.updateHover3()
			d.Invalidate()
		}
		return
	}
	switch ev.Button {
	case 1:
		if ev.Kind == MouseDown {
			a.updateHover3()
			if a.hover3Valid && a.world.At3(a.hover3X, a.hover3Y, a.hover3Z) == bwBedrock {
				a.setStatus("基岩非常坚硬，无法破坏")
				return
			}
			a.mining = a.hover3Valid
			a.mineProg = 0
			a.mine3X, a.mine3Y, a.mine3Z = a.hover3X, a.hover3Y, a.hover3Z
			d.Invalidate()
		} else if ev.Kind == MouseUp {
			a.mining = false
			a.mineProg = 0
			d.Invalidate()
		}
	case 3:
		if ev.Kind == MouseDown {
			a.mouseLooking = true
			a.lastMouseX, a.lastMouseY = ev.X, ev.Y
		} else if ev.Kind == MouseUp {
			a.mouseLooking = false
			a.updateHover3()
			if a.hover3Valid {
				a.placeBlock3(a.hover3PX, a.hover3PY, a.hover3PZ)
			}
			d.Invalidate()
		}
	}
}

func (a *BlockWorldApp) tileAt(client ui.Rect, sx, sy int) (int, int, bool) {
	wx := sx - client.X + a.camX
	wy := sy - client.Y + a.camY
	if wx < 0 || wy < 0 {
		return 0, 0, false
	}
	tx, ty := wx/bwTile, wy/bwTile
	if tx < 0 || tx >= a.world.W || ty < 0 || ty >= a.world.H {
		return 0, 0, false
	}
	return tx, ty, true
}

func (a *BlockWorldApp) updateCamera(client ui.Rect) {
	a.client = client
	worldW := a.world.W * bwTile
	worldH := a.world.H * bwTile
	camX := int(a.px+bwPW/2) - client.W/2
	camY := int(a.py+bwPH/2) - client.H/2
	if worldW <= client.W {
		camX = -((client.W - worldW) / 2)
	} else if camX < 0 {
		camX = 0
	} else if camX > worldW-client.W {
		camX = worldW - client.W
	}
	if worldH <= client.H {
		camY = -((client.H - worldH) / 2)
	} else if camY < 0 {
		camY = 0
	} else if camY > worldH-client.H {
		camY = worldH - client.H
	}
	a.camX, a.camY = camX, camY
}

func hotbarRect(client ui.Rect, i int) ui.Rect {
	const size, gap = 30, 2
	total := 9*size + 8*gap
	x0 := client.X + (client.W-total)/2
	y0 := client.Bottom() - size - 8
	return ui.Rect{x0 + i*(size+gap), y0, size, size}
}

func (a *BlockWorldApp) Paint(d *Desktop, w *Window, c *ui.Canvas, client ui.Rect) {
	a.client = client
	f := ui.DefaultFont()
	if a.view3D {
		a.render3D(c, client)
		a.drawCrosshair3(c, client, f)
		a.drawHUD(c, client, f)
		if a.craftOpen {
			a.drawCraftPanel(c, client, f)
		}
		return
	}
	a.updateCamera(client)
	a.drawSky(c, client)

	x0 := a.camX / bwTile
	x1 := (a.camX + client.W) / bwTile
	y0 := a.camY / bwTile
	y1 := (a.camY + client.H) / bwTile
	if x0 < 0 {
		x0 = 0
	}
	if y0 < 0 {
		y0 = 0
	}
	if x1 >= a.world.W {
		x1 = a.world.W - 1
	}
	if y1 >= a.world.H {
		y1 = a.world.H - 1
	}
	for ty := y0; ty <= y1; ty++ {
		for tx := x0; tx <= x1; tx++ {
			px := client.X + tx*bwTile - a.camX
			py := client.Y + ty*bwTile - a.camY
			b := a.world.At(tx, ty)
			if b == bwAir {
				if ty >= a.world.Heights[tx] {
					depth := ty - a.world.Heights[tx]
					col := bwCaveColor(depth)
					c.FillRect(ui.Rect{px, py, bwTile, bwTile}, col)
					h := bwHash(uint32(tx)^uint32(a.world.Seed), uint32(ty))
					c.Set(px+int(h%13)+1, py+int((h>>8)%13)+1, bwShade(col, 0.8))
					c.Set(px+int((h>>16)%13)+1, py+int((h>>4)%13)+1, bwShade(col, 1.18))
				}
				continue
			}
			seed := uint32(a.world.Seed) ^ uint32(tx)<<8 ^ uint32(ty)
			if a.view3D {
				a.drawBlock3D(c, b, px, py, seed, tx, ty)
			} else {
				drawBWBlock(c, b, px, py, seed)
			}
		}
	}

	for _, p := range a.particles {
		sx := client.X + int(p.x) - a.camX
		sy := client.Y + int(p.y) - a.camY
		if sx < client.X || sx >= client.Right() || sy < client.Y || sy >= client.Bottom() {
			continue
		}
		c.FillRect(ui.Rect{sx, sy, p.size, p.size}, p.col)
	}

	a.drawPlayer(c, client)
	a.drawHover(c, client)
	a.drawFloats(c, client, f)
	a.drawHUD(c, client, f)
	if a.craftOpen {
		a.drawCraftPanel(c, client, f)
	}
}

func (a *BlockWorldApp) drawSky(c *ui.Canvas, client ui.Rect) {
	top, bot := bwSkyColors(a.phase)
	band := 8
	for y := client.Y; y < client.Bottom(); y += band {
		h := band
		if y+h > client.Bottom() {
			h = client.Bottom() - y
		}
		t := float64(y-client.Y) / float64(client.H)
		c.FillRect(ui.Rect{client.X, y, client.W, h}, bwLerp(top, bot, t))
	}
	night := bwNightAmount(a.phase)
	if night > 0.02 {
		for i := 0; i < 90; i++ {
			h := bwHash(uint32(i), uint32(a.seed))
			sx := client.X + int(h%uint32(client.W))
			sy := client.Y + int((h>>8)%uint32(client.H*3/5))
			col := bwLerp(0x0A1020, 0xFFFFFF, night*(0.4+float64((h>>16)&0xFF)/420))
			c.Set(sx, sy, col)
			if h&0x10000 != 0 {
				c.Set(sx+1, sy, col)
			}
		}
	}
	if a.phase < 0.5 {
		p := a.phase / 0.5
		sx := client.X + int(p*float64(client.W))
		sy := client.Y + 96 - int(math.Sin(p*math.Pi)*64)
		sx = min(max(sx, client.X+12), client.Right()-12)
		drawBWDisc(c, sx, sy, 11, 0xFFE060, 0xFFF6B0)
	} else {
		p := (a.phase - 0.5) / 0.5
		sx := client.X + int(p*float64(client.W))
		sy := client.Y + 96 - int(math.Sin(p*math.Pi)*64)
		sx = min(max(sx, client.X+12), client.Right()-12)
		drawBWDisc(c, sx, sy, 9, 0xC8D0E8, 0xF0F4FF)
		c.Set(sx-2, sy-1, 0x9FA8C0)
		c.Set(sx+2, sy+2, 0x9FA8C0)
	}
	light := 1 - night
	cloud := bwLerp(0x283040, 0xF8F8FF, light)
	for i := 0; i < 6; i++ {
		cw := 34 + (i%3)*14
		x := (i*260 + int(a.cloudShift*3)) % (client.W + 160)
		x = client.X + x - 80
		y := client.Y + 34 + (i%4)*26
		drawBWCloudPart(c, client, x, y, cw, 7, cloud)
		drawBWCloudPart(c, client, x+8, y-5, cw-18, 6, cloud)
	}
}

func drawBWCloudPart(c *ui.Canvas, client ui.Rect, x, y, w, h int, col uint32) {
	x0, x1 := x, x+w
	if x1 <= client.X || x0 >= client.Right() {
		return
	}
	if x0 < client.X {
		x0 = client.X
	}
	if x1 > client.Right() {
		x1 = client.Right()
	}
	c.FillRect(ui.Rect{x0, y, x1 - x0, h}, col)
}

func drawBWDisc(c *ui.Canvas, cx, cy, r int, rim, core uint32) {
	for y := -r; y <= r; y++ {
		for x := -r; x <= r; x++ {
			if x*x+y*y > r*r {
				continue
			}
			col := rim
			if x*x+y*y <= (r-3)*(r-3) {
				col = core
			}
			c.Set(cx+x, cy+y, col)
		}
	}
}

func bwSkyColors(phase float64) (uint32, uint32) {
	type key struct {
		at       float64
		top, bot uint32
	}
	keys := []key{
		{0.00, 0x1C2A52, 0xE08A60},
		{0.08, 0x5FA8F0, 0xC8E8FF},
		{0.42, 0x5FA8F0, 0xC8E8FF},
		{0.50, 0x302850, 0xE07850},
		{0.58, 0x050A16, 0x0C1426},
		{0.90, 0x050A16, 0x0C1426},
		{1.00, 0x1C2A52, 0xE08A60},
	}
	p := math.Mod(math.Abs(phase), 1)
	for i := 0; i < len(keys)-1; i++ {
		a, b := keys[i], keys[i+1]
		if p >= a.at && p <= b.at {
			t := (p - a.at) / (b.at - a.at)
			return bwLerp(a.top, b.top, t), bwLerp(a.bot, b.bot, t)
		}
	}
	return keys[0].top, keys[0].bot
}

func bwNightAmount(phase float64) float64 {
	p := math.Mod(math.Abs(phase), 1)
	switch {
	case p < 0.45:
		return 0
	case p < 0.56:
		return (p - 0.45) / 0.11
	case p < 0.90:
		return 1
	default:
		return 1 - (p-0.90)/0.10
	}
}

func bwCaveColor(depth int) uint32 {
	t := float64(depth) / 30
	if t > 1 {
		t = 1
	}
	return bwLerp(0x4C4640, 0x2A2622, t)
}

func bwLerp(a, b uint32, t float64) uint32 {
	if t < 0 {
		t = 0
	}
	if t > 1 {
		t = 1
	}
	ar, ag, ab := (a>>16)&0xFF, (a>>8)&0xFF, a&0xFF
	br, bg, bb := (b>>16)&0xFF, (b>>8)&0xFF, b&0xFF
	r := int(float64(ar) + (float64(br)-float64(ar))*t)
	g := int(float64(ag) + (float64(bg)-float64(ag))*t)
	bl := int(float64(ab) + (float64(bb)-float64(ab))*t)
	return uint32(r)<<16 | uint32(g)<<8 | uint32(bl)
}

func bwShade(col uint32, f float64) uint32 {
	r := int(float64((col>>16)&0xFF) * f)
	g := int(float64((col>>8)&0xFF) * f)
	b := int(float64(col&0xFF) * f)
	if r > 255 {
		r = 255
	}
	if g > 255 {
		g = 255
	}
	if b > 255 {
		b = 255
	}
	return uint32(r)<<16 | uint32(g)<<8 | uint32(b)
}

func bwHash(x, y uint32) uint32 {
	h := x*0x9E3779B1 + y*0x85EBCA77 + 0x165667B1
	h ^= h >> 15
	h *= 0x2C1B3C6D
	h ^= h >> 12
	return h
}

func drawBWBlock(c *ui.Canvas, b bwBlock, px, py int, seed uint32) {
	r := ui.Rect{px, py, bwTile, bwTile}
	speck := func(i int, col uint32) {
		h := bwHash(seed, uint32(i))
		c.Set(px+int(h%13)+1, py+int((h>>8)%13)+1, col)
	}
	switch b {
	case bwGrass:
		c.FillRect(r, 0x7A5533)
		c.FillRect(ui.Rect{px, py, bwTile, 4}, 0x4CA030)
		c.FillRect(ui.Rect{px, py + 4, bwTile, 1}, 0x2F7A22)
		for i := 0; i < 5; i++ {
			speck(i, 0x8A6540)
			speck(i+7, 0x6A4528)
		}
		c.Set(px+2, py+4, 0x6ABF48)
		c.Set(px+11, py+3, 0x6ABF48)
	case bwDirt:
		c.FillRect(r, 0x7A5533)
		for i := 0; i < 7; i++ {
			speck(i, 0x6A4528)
			speck(i+11, 0x8A6540)
		}
	case bwStone:
		c.FillRect(r, 0x808080)
		for i := 0; i < 6; i++ {
			speck(i, 0x6E6E6E)
			speck(i+9, 0x929292)
		}
		c.HLine(px+3, py+10, 4, 0x686868)
		c.HLine(px+9, py+5, 3, 0x686868)
	case bwCobble:
		c.FillRect(r, 0x666666)
		c.FillRect(ui.Rect{px + 1, py + 1, 6, 6}, 0x8A8A8A)
		c.FillRect(ui.Rect{px + 9, py + 1, 6, 6}, 0x7A7A7A)
		c.FillRect(ui.Rect{px + 1, py + 9, 6, 6}, 0x7A7A7A)
		c.FillRect(ui.Rect{px + 9, py + 9, 6, 6}, 0x8E8E8E)
		c.Set(px+2, py+2, 0xA0A0A0)
		c.Set(px+10, py+10, 0xA0A0A0)
	case bwSand:
		c.FillRect(r, 0xD8C48A)
		for i := 0; i < 8; i++ {
			speck(i, 0xC0A870)
			speck(i+9, 0xE8D8A8)
		}
	case bwWood:
		c.FillRect(r, 0x6B4A2A)
		c.VLine(px+3, py, bwTile, 0x50341C)
		c.VLine(px+9, py, bwTile, 0x50341C)
		c.Set(px+6, py+5, 0x4A3018)
		c.Set(px+7, py+6, 0x4A3018)
		c.Set(px+12, py+11, 0x805A34)
	case bwLeaves:
		c.FillRect(r, 0x2E7A2E)
		for i := 0; i < 7; i++ {
			speck(i, 0x3E9A3E)
			speck(i+8, 0x1E5A1E)
		}
		c.FillRect(ui.Rect{px + 3, py + 3, 3, 2}, 0x4AA84A)
		c.FillRect(ui.Rect{px + 10, py + 9, 3, 2}, 0x4AA84A)
	case bwCoal, bwIron, bwGold, bwDiamond:
		c.FillRect(r, 0x808080)
		for i := 0; i < 5; i++ {
			speck(i, 0x6E6E6E)
		}
		ore := uint32(0x2A2A2A)
		hi := uint32(0x505050)
		switch b {
		case bwIron:
			ore, hi = 0xC08060, 0xE0B090
		case bwGold:
			ore, hi = 0xE0B020, 0xFFE070
		case bwDiamond:
			ore, hi = 0x40C8C8, 0x90F0F0
		}
		for i := 0; i < 5; i++ {
			h := bwHash(seed, uint32(i+31))
			x := px + 2 + int(h%11)
			y := py + 2 + int((h>>8)%11)
			c.FillRect(ui.Rect{x, y, 2, 2}, ore)
			c.Set(x, y, hi)
		}
	case bwPlanks:
		c.FillRect(r, 0xB08050)
		c.HLine(px, py+5, bwTile, 0x8A6038)
		c.HLine(px, py+11, bwTile, 0x8A6038)
		c.VLine(px+8, py, 5, 0x8A6038)
		c.VLine(px+4, py+6, 5, 0x8A6038)
		c.VLine(px+12, py+12, 4, 0x8A6038)
		c.Set(px+2, py+2, 0xC89868)
	case bwGlass:
		c.FillRect(r, 0x9FD8F0)
		c.Frame(r, 0xC8EEFF)
		c.HLine(px+3, py+3, 5, 0xE8F8FF)
		c.Set(px+4, py+4, 0xE8F8FF)
		c.HLine(px+9, py+11, 4, 0xC0E8FF)
	case bwBedrock:
		c.FillRect(r, 0x4A4A4A)
		for i := 0; i < 6; i++ {
			speck(i, 0x303030)
			speck(i+7, 0x606060)
		}
		c.FillRect(ui.Rect{px + 2, py + 8, 3, 2}, 0x262626)
		c.FillRect(ui.Rect{px + 10, py + 3, 3, 2}, 0x262626)
	case bwCraftingTable:
		c.FillRect(r, 0x9C7040)
		c.FillRect(ui.Rect{px, py, bwTile, 4}, 0xB08050)
		c.HLine(px, py+4, bwTile, 0x6A4A28)
		c.Frame(ui.Rect{px + 2, py + 6, 5, 5}, 0x50341C)
		c.Frame(ui.Rect{px + 9, py + 6, 5, 5}, 0x50341C)
		c.Set(px+4, py+8, 0xC89868)
		c.Set(px+11, py+8, 0xC89868)
		c.VLine(px+7, py+6, 8, 0x6A4A28)
		c.Set(px+3, py+1, 0xC89868)
		c.Set(px+12, py+2, 0xC89868)
	case bwBrick:
		c.FillRect(r, 0xA84838)
		c.HLine(px, py+4, bwTile, 0xD8D0C8)
		c.HLine(px, py+9, bwTile, 0xD8D0C8)
		c.HLine(px, py+14, bwTile, 0xD8D0C8)
		c.VLine(px+8, py, 4, 0xD8D0C8)
		c.VLine(px+4, py+5, 4, 0xD8D0C8)
		c.VLine(px+12, py+5, 4, 0xD8D0C8)
		c.VLine(px+8, py+10, 4, 0xD8D0C8)
		c.Set(px+2, py+2, 0xC06048)
		c.Set(px+10, py+11, 0xC06048)
	}
}

func (a *BlockWorldApp) drawPlayer(c *ui.Canvas, client ui.Rect) {
	sx := client.X + int(a.px) - a.camX
	sy := client.Y + int(a.py) - a.camY
	if sx+bwTile < client.X || sx > client.Right() || sy+bwTile < client.Y || sy > client.Bottom() {
		return
	}
	skin := uint32(0xE8B080)
	hair := uint32(0x40200F)
	shirt := uint32(0x30A8C8)
	pants := uint32(0x303C80)
	shoe := uint32(0x282828)
	swing := 0
	if a.vx != 0 && a.onGround {
		swing = int(math.Sin(a.walk) * 2)
	}
	c.FillRect(ui.Rect{sx + 2, sy + 18 + max(0, swing), 3, 8}, pants)
	c.FillRect(ui.Rect{sx + 7, sy + 18 + max(0, -swing), 3, 8}, pants)
	c.FillRect(ui.Rect{sx + 2, sy + 25 + max(0, swing), 3, 1}, shoe)
	c.FillRect(ui.Rect{sx + 7, sy + 25 + max(0, -swing), 3, 1}, shoe)
	c.FillRect(ui.Rect{sx + 2, sy + 8, 8, 10}, shirt)
	c.FillRect(ui.Rect{sx, sy + 9 - swing, 2, 8}, shirt)
	c.FillRect(ui.Rect{sx + 10, sy + 9 + swing, 2, 8}, shirt)
	c.FillRect(ui.Rect{sx, sy + 16 - swing, 2, 2}, skin)
	c.FillRect(ui.Rect{sx + 10, sy + 16 + swing, 2, 2}, skin)
	c.FillRect(ui.Rect{sx + 1, sy, 10, 8}, skin)
	c.FillRect(ui.Rect{sx + 1, sy, 10, 3}, hair)
	c.FillRect(ui.Rect{sx + 1, sy + 3, 1, 3}, hair)
	c.FillRect(ui.Rect{sx + 10, sy + 3, 1, 3}, hair)
	eyeX := sx + 3
	if a.face > 0 {
		eyeX = sx + 7
	}
	c.Set(eyeX, sy+4, 0xFFFFFF)
	c.Set(eyeX+1, sy+4, 0x2050A0)
	c.FillRect(ui.Rect{sx + 4, sy + 7, 4, 1}, 0xC88868)
}

func (a *BlockWorldApp) drawHover(c *ui.Canvas, client ui.Rect) {
	if !a.hoverValid {
		return
	}
	tx, ty := a.hoverX, a.hoverY
	if tx < 0 || tx >= a.world.W || ty < 0 || ty >= a.world.H {
		return
	}
	px := client.X + tx*bwTile - a.camX
	py := client.Y + ty*bwTile - a.camY
	if px+bwTile < client.X || px > client.Right() || py+bwTile < client.Y || py > client.Bottom() {
		return
	}
	inReach := a.withinReach(tx, ty)
	col := uint32(0x808080)
	if inReach {
		col = 0xF0F0A0
	}
	c.Frame(ui.Rect{px, py, bwTile, bwTile}, col)
	if !inReach {
		return
	}
	b := a.world.At(tx, ty)
	if a.mining && tx == a.mineTx && ty == a.mineTy && b != bwAir {
		prog := 0.0
		if h := bwHard[b]; h > 0 {
			prog = a.mineProg / h
		}
		if prog > 0 {
			if prog > 0.2 {
				c.HLine(px+3, py+5, 5, 0x202020)
			}
			if prog > 0.45 {
				c.VLine(px+8, py+4, 5, 0x202020)
			}
			if prog > 0.7 {
				c.HLine(px+6, py+11, 6, 0x202020)
			}
			bw := 24
			bx := px + (bwTile-bw)/2
			by := py - 6
			c.FillRect(ui.Rect{bx, by, bw, 4}, 0x202020)
			c.FillRect(ui.Rect{bx + 1, by + 1, int(float64(bw-2) * prog), 2}, 0xE8D040)
		}
	}
}

func (a *BlockWorldApp) drawHUD(c *ui.Canvas, client ui.Rect, f *ui.Font) {
	panel := ui.Rect{client.X + 6, client.Y + 6, 270, 58}
	c.FillRect(panel, 0x141C26)
	c.Frame(panel, 0x506070)
	c.FillRect(ui.Rect{panel.X + 1, panel.Y + 1, 3, panel.H - 2}, 0x40A060)
	label := "方块世界 95"
	f.TextBold(c, panel.X+10, panel.Y+5, label, 0xFFFF80)
	tx := int(a.px / bwTile)
	ty := int((a.py + bwPH) / bwTile)
	tz := int((a.pz + bwPW/2) / bwTile)
	period := "白天"
	switch {
	case a.phase < 0.08 || a.phase >= 0.94:
		period = "黎明"
	case a.phase < 0.5:
		period = "白天"
	case a.phase < 0.6:
		period = "黄昏"
	default:
		period = "夜晚"
	}
	if a.view3D {
		f.Text(c, panel.X+10, panel.Y+21, fmt.Sprintf("X %d   Y %d   Z %d   ·   %s", tx, ty, tz, period), 0xC8D8E8)
		f.Text(c, panel.X+10, panel.Y+37, "WASD 移动 · 右键拖动视角 · V 剖面", 0x8FA6B8)
	} else {
		f.Text(c, panel.X+10, panel.Y+21, fmt.Sprintf("X %d   Y %d   ·   %s", tx, ty, period), 0xC8D8E8)
		f.Text(c, panel.X+10, panel.Y+37, "A/D 移动 · 左键挖 · 右键放 · V 3D", 0x8FA6B8)
	}

	for i := 0; i < 9; i++ {
		r := hotbarRect(client, i)
		if i == a.sel {
			c.FillRect(r, 0x303848)
			c.Frame(r, 0xFFFFFF)
			c.Frame(ui.Rect{r.X + 1, r.Y + 1, r.W - 2, r.H - 2}, 0x808080)
		} else {
			c.FillRect(r, 0x202830)
			c.Sunken(r)
		}
		slot := a.hotbar[i]
		if slot.Count > 0 {
			drawBWBlock(c, slot.Block, r.X+(r.W-bwTile)/2, r.Y+1, uint32(i*97+13))
			f.TextRight(c, r.Right()-2, r.Bottom()-12, fmt.Sprintf("%d", slot.Count), 0xFFFFFF)
		}
	}
	if a.sel >= 0 && a.sel < 9 && a.hotbar[a.sel].Count > 0 {
		name := bwNames[a.hotbar[a.sel].Block]
		nw := f.Width(name)
		nx := client.X + client.W/2 - nw/2
		ny := hotbarRect(client, 0).Y - 22
		c.FillRect(ui.Rect{nx - 6, ny - 2, nw + 12, 20}, 0x141C26)
		c.Frame(ui.Rect{nx - 6, ny - 2, nw + 12, 20}, 0x506070)
		f.Text(c, nx, ny, name, 0xF0F0C0)
	}
	if a.status != "" && time.Now().Before(a.statusUntil) {
		maxW := client.Right() - panel.X - 16
		nw := f.Width(a.status)
		if nw > maxW {
			nw = maxW
		}
		nx := panel.X
		ny := panel.Bottom() + 6
		c.FillRect(ui.Rect{nx - 8, ny - 4, nw + 16, 24}, 0x203040)
		c.Frame(ui.Rect{nx - 8, ny - 4, nw + 16, 24}, 0x8090A0)
		f.TextClip(c, nx, ny, nw, a.status, 0xFFFFC0)
	}
}
