package desktop

import (
	"path/filepath"
	"testing"
	"time"

	"win95linux/internal/ui"
)

func TestBWWorldGeneration(t *testing.T) {
	w := newBWWorld(2026)
	counts := map[bwBlock]int{}
	for x := 0; x < w.W; x++ {
		if w.Heights[x] < 20 || w.Heights[x] >= w.H-2 {
			t.Fatalf("column %d height %d out of range", x, w.Heights[x])
		}
		for y := 0; y < w.H; y++ {
			b := w.At(x, y)
			counts[b]++
			if b < bwAir || b > bwBedrock {
				t.Fatalf("invalid block %d at %d,%d", b, x, y)
			}
		}
		if w.At(x, w.H-1) != bwBedrock {
			t.Fatalf("column %d bottom is not bedrock", x)
		}
	}
	for _, b := range []bwBlock{bwGrass, bwDirt, bwStone, bwWood, bwLeaves, bwCoal} {
		if counts[b] == 0 {
			t.Fatalf("generated world is missing %s", bwNames[b])
		}
	}
}

func TestBWRLE(t *testing.T) {
	w := newBWWorld(7)
	data := w.rle()
	got, err := bwDecodeRLE(data, len(w.Tiles))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != len(w.Tiles) {
		t.Fatalf("decoded %d tiles, want %d", len(got), len(w.Tiles))
	}
	for i := range got {
		if got[i] != w.Tiles[i] {
			t.Fatalf("tile %d = %d, want %d", i, got[i], w.Tiles[i])
		}
	}
}

func TestBWBreakBlockAddsItem(t *testing.T) {
	a := NewBlockWorldApp(nil)
	a.savePath = filepath.Join(t.TempDir(), "save.json")
	x := int(a.px / bwTile)
	y := a.world.columnTop(x)
	b := a.world.At(x, y)
	if b != bwGrass && b != bwSand {
		t.Fatalf("spawn column top is %s", bwNames[b])
	}
	before := a.hotbarCount(bwDirt)
	if !a.breakBlock(x, y) {
		t.Fatal("breakBlock returned false")
	}
	if a.world.At(x, y) != bwAir {
		t.Fatal("block was not removed")
	}
	if b == bwGrass && a.hotbarCount(bwDirt) != before+1 {
		t.Fatalf("dirt count = %d, want %d", a.hotbarCount(bwDirt), before+1)
	}
	if !a.dirty {
		t.Fatal("world should be marked dirty")
	}
}

func (a *BlockWorldApp) hotbarCount(b bwBlock) int {
	n := 0
	for _, s := range a.hotbar {
		if s.Block == b {
			n += s.Count
		}
	}
	return n
}

func TestBWPlaceBlock(t *testing.T) {
	a := NewBlockWorldApp(nil)
	a.savePath = filepath.Join(t.TempDir(), "save.json")
	a.sel = 1
	start := a.hotbar[1].Count
	if start == 0 || a.hotbar[1].Block != bwDirt {
		t.Fatalf("slot 1 = %+v, want dirt", a.hotbar[1])
	}
	px := int(a.px / bwTile)
	py := int(a.py / bwTile)
	found := false
	var tx, ty int
	for dy := -4; dy <= 4 && !found; dy++ {
		for dx := -4; dx <= 4 && !found; dx++ {
			x, y := px+dx, py+dy
			if a.world.At(x, y) != bwAir || !a.withinReach(x, y) || a.blockHitsPlayer(x, y) {
				continue
			}
			if a.world.At(x-1, y) == bwAir && a.world.At(x+1, y) == bwAir &&
				a.world.At(x, y-1) == bwAir && a.world.At(x, y+1) == bwAir {
				continue
			}
			tx, ty, found = x, y, true
		}
	}
	if !found {
		t.Skip("no suitable placement tile near spawn")
	}
	a.placeBlock(tx, ty)
	if a.world.At(tx, ty) != bwDirt {
		t.Fatalf("tile %d,%d = %s, want dirt", tx, ty, bwNames[a.world.At(tx, ty)])
	}
	if a.hotbar[1].Count != start-1 {
		t.Fatalf("count = %d, want %d", a.hotbar[1].Count, start-1)
	}
}

func TestBWSaveLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	a := NewBlockWorldApp(nil)
	a.savePath = filepath.Join(dir, "world.json")
	x := int(a.px / bwTile)
	y := a.world.columnTop(x)
	a.breakBlock(x, y)
	a.px += 32
	a.phase = 0.42
	a.sel = 4
	if err := a.writeSave(); err != nil {
		t.Fatal(err)
	}
	b := NewBlockWorldApp(nil)
	b.savePath = a.savePath
	b.loadWorld(false)
	if b.world == nil || b.world.Seed != a.world.Seed {
		t.Fatal("seed was not restored")
	}
	if b.world.At(x, y) != bwAir {
		t.Fatal("modified tile was not restored")
	}
	if b.px != a.px {
		t.Fatalf("player x = %v, want %v", b.px, a.px)
	}
	if b.sel != a.sel {
		t.Fatalf("selected slot = %d, want %d", b.sel, a.sel)
	}
	if b.hotbarCount(bwDirt) != a.hotbarCount(bwDirt) {
		t.Fatal("hotbar was not restored")
	}
	if !b.withinReach(int(b.px/bwTile), int(b.py/bwTile)) {
		t.Fatal("restored player is outside the map")
	}
}

func TestBWUnbreakableBedrock(t *testing.T) {
	a := NewBlockWorldApp(nil)
	if a.breakBlock(1, a.world.H-1) {
		t.Fatal("bedrock should not break")
	}
	if a.world.At(1, a.world.H-1) != bwBedrock {
		t.Fatal("bedrock was removed")
	}
}

func TestBWPhysicsLandsAndJumps(t *testing.T) {
	a := NewBlockWorldApp(nil)
	d := &Desktop{}
	x := int(a.px / bwTile)
	top := a.world.columnTop(x)
	a.py = float64((top-6)*bwTile) - bwPH
	a.onGround = false
	now := time.Now()
	for i := 0; i < 150; i++ {
		a.Animate(d, nil, now.Add(time.Duration(i+1)*40*time.Millisecond))
	}
	if !a.onGround {
		t.Fatalf("player did not land (y=%v)", a.py)
	}
	landed := a.py
	a.jump()
	if a.vy >= 0 {
		t.Fatalf("jump velocity = %v, want negative", a.vy)
	}
	for i := 0; i < 5; i++ {
		a.Animate(d, nil, now.Add(time.Duration(260+i*40)*time.Millisecond))
	}
	if a.py >= landed {
		t.Fatalf("player did not rise after jump (y=%v, landed=%v)", a.py, landed)
	}
}

func TestBWMiningProgressBreaksBlock(t *testing.T) {
	a := NewBlockWorldApp(nil)
	x := int(a.px / bwTile)
	y := a.world.columnTop(x)
	b := a.world.At(x, y)
	if b != bwGrass && b != bwSand {
		t.Skip("spawn is not on soil")
	}
	a.hoverX, a.hoverY, a.hoverValid = x, y, true
	a.mineTx, a.mineTy = -1, -1
	a.mining = true
	for i := 0; i < 30 && a.world.At(x, y) != bwAir; i++ {
		a.stepMining(0.1)
	}
	if a.world.At(x, y) != bwAir {
		t.Fatal("mining did not break the block")
	}
	if a.mineProg != 0 {
		t.Fatalf("mine progress = %v after break, want 0", a.mineProg)
	}
}

func TestBWCraftPlanksFromWood(t *testing.T) {
	a := NewBlockWorldApp(nil)
	beforeWood := a.hotbarCountOf(bwWood)
	beforePlanks := a.hotbarCountOf(bwPlanks)
	if beforeWood < 1 {
		t.Fatal("expected starting wood")
	}
	if !a.canCraft(0) {
		t.Fatal("planks recipe should be craftable anywhere")
	}
	a.doCraft(0)
	if a.hotbarCountOf(bwWood) != beforeWood-1 {
		t.Fatalf("wood = %d, want %d", a.hotbarCountOf(bwWood), beforeWood-1)
	}
	if a.hotbarCountOf(bwPlanks) != beforePlanks+4 {
		t.Fatalf("planks = %d, want %d", a.hotbarCountOf(bwPlanks), beforePlanks+4)
	}
}

func TestBWCraftRequiresTable(t *testing.T) {
	a := NewBlockWorldApp(nil)
	// Recipe 3 is glass (sand×2 → glass×2) and needs a placed table.
	if a.canCraft(3) {
		t.Fatal("glass should not be craftable without a nearby table")
	}
	if a.hotbarCountOf(bwSand) < 2 {
		t.Fatal("expected starting sand")
	}
	cx := int((a.px + bwPW/2) / bwTile)
	cy := int((a.py + bwPH/2) / bwTile)
	a.world.Set(cx+1, cy, bwCraftingTable)
	if !a.nearTable() {
		t.Fatal("placed crafting table was not detected")
	}
	if !a.canCraft(3) {
		t.Fatal("glass should be craftable next to a table")
	}
	beforeSand := a.hotbarCountOf(bwSand)
	beforeGlass := a.hotbarCountOf(bwGlass)
	a.doCraft(3)
	if a.hotbarCountOf(bwSand) != beforeSand-2 {
		t.Fatalf("sand = %d, want %d", a.hotbarCountOf(bwSand), beforeSand-2)
	}
	if a.hotbarCountOf(bwGlass) != beforeGlass+2 {
		t.Fatalf("glass = %d, want %d", a.hotbarCountOf(bwGlass), beforeGlass+2)
	}
}

func TestBWNewBlocksValid(t *testing.T) {
	if bwNames[bwCraftingTable] != "工作台" || bwNames[bwBrick] != "砖块" {
		t.Fatalf("new block names wrong: %q %q", bwNames[bwCraftingTable], bwNames[bwBrick])
	}
	if bwHard[bwCraftingTable] <= 0 || bwHard[bwBrick] <= 0 {
		t.Fatal("new blocks should be mineable")
	}
	w := newBWWorld(3)
	w.Set(5, 5, bwCraftingTable)
	w.Set(6, 5, bwBrick)
	got, err := bwDecodeRLE(w.rle(), len(w.Tiles))
	if err != nil {
		t.Fatal(err)
	}
	if got[5*w.W+5] != bwCraftingTable || got[5*w.W+6] != bwBrick {
		t.Fatal("new blocks did not survive RLE round trip")
	}
	if _, err := bwDecodeRLE([]byte{byte(bwBlockCount), 1}, 1); err == nil {
		t.Fatal("out-of-range block should be rejected")
	}
}

func TestBWPaintModes(t *testing.T) {
	a := NewBlockWorldApp(nil)
	a.addFloat(int(a.px/bwTile), int(a.py/bwTile), "+1 泥土", 0xF0F0A0)
	for _, mode := range []bool{false, true} {
		a.view3D = mode
		a.craftOpen = mode
		c := ui.NewCanvas(960, 640)
		a.Paint(nil, nil, c, ui.Rect{0, 0, 960, 640})
	}
}

func TestBWFloatTextLifecycle(t *testing.T) {
	a := NewBlockWorldApp(nil)
	a.addFloat(10, 10, "测试", 0xFFFFFF)
	if len(a.floats) != 1 {
		t.Fatalf("floats = %d, want 1", len(a.floats))
	}
	a.stepFloats(0.5)
	if len(a.floats) != 1 {
		t.Fatal("float should still be alive")
	}
	a.stepFloats(1.0)
	if len(a.floats) != 0 {
		t.Fatal("float should have expired")
	}
}
