package desktop

import (
	"win95linux/internal/ui"
)

// MaybeShowWelcome opens the first-boot welcome + license agreement exactly
// once. Acceptance is persisted in desktop.json, so later boots skip it.
func (d *Desktop) MaybeShowWelcome() {
	if d.Mode != ModeDesktop || d.eulaAccepted {
		return
	}
	a := newWelcomeApp(d)
	w := d.OpenModal(a)
	a.win = w
}

type welcomeApp struct {
	Base
	d      *Desktop
	win    *Window
	lines  []string
	scroll VScroll

	textR, scrollR    ui.Rect
	acceptR, declineR ui.Rect
	press             string
}

func newWelcomeApp(d *Desktop) *welcomeApp {
	return &welcomeApp{d: d}
}

func (a *welcomeApp) Title() string           { return "欢迎使用 Windows 95 Linux" }
func (a *welcomeApp) Icon() ui.IconKind       { return ui.IcoSetup }
func (a *welcomeApp) DefaultSize() (int, int) { return 660, 540 }
func (a *welcomeApp) MinSize() (int, int)     { return 660, 540 }
func (a *welcomeApp) Resizable() bool         { return false }

func (a *welcomeApp) Resized(d *Desktop, w *Window, client ui.Rect) {
	const margin, sbW, headerH, footerH = 10, 16, 84, 56
	area := ui.Rect{client.X + margin, client.Y + headerH, client.W - 2*margin, client.H - headerH - footerH}
	a.textR = ui.Rect{area.X, area.Y, area.W - sbW, area.H}
	a.scrollR = ui.Rect{area.Right() - sbW, area.Y, sbW, area.H}
	a.lines = wrapText(apacheLicense, a.textR.W-16)
	a.scroll.Total = len(a.lines)
	a.scroll.Visible = (a.textR.H - 8) / welcomeLineH
	a.scroll.Clamp()
	by := client.Bottom() - footerH + 14
	a.acceptR = ui.Rect{client.X + client.W/2 - 150, by, 140, 28}
	a.declineR = ui.Rect{client.X + client.W/2 + 10, by, 140, 28}
}

const welcomeLineH = 18

func (a *welcomeApp) Paint(d *Desktop, w *Window, c *ui.Canvas, client ui.Rect) {
	f := ui.DefaultFont()
	c.FillRect(client, ui.ColFace)
	// Header banner.
	hb := ui.Rect{client.X, client.Y, client.W, 74}
	c.TitleBar(hb, ui.ColNavy, ui.ColNavyLight)
	ui.DrawIcon32(c, ui.IcoSetup, client.X+14, client.Y+18, false)
	f.TextBold(c, client.X+56, client.Y+14, "欢迎使用 Windows 95 Linux 26.2", ui.ColWhite)
	f.Text(c, client.X+56, client.Y+34, "首次启动 · 请阅读并接受以下许可协议", 0xD0E0F0)
	f.Text(c, client.X+56, client.Y+52, "接受后本协议不会再次弹出；拒绝则下次启动仍会显示。", 0xB0C8E0)

	// Read-only license text.
	c.FillRect(a.textR, ui.ColWhite)
	c.Sunken(a.textR)
	for i := a.scroll.Pos; i < len(a.lines) && i < a.scroll.Pos+a.scroll.Visible; i++ {
		y := a.textR.Y + 4 + (i-a.scroll.Pos)*welcomeLineH
		if y+welcomeLineH > a.textR.Bottom()-2 {
			break
		}
		f.TextClip(c, a.textR.X+8, y, a.textR.W-16, a.lines[i], ui.ColText)
	}
	a.scroll.Draw(c, a.scrollR)

	// Footer buttons.
	f.Text(c, client.X+12, client.Bottom()-40, "Apache License 2.0", 0x606060)
	ui.Button(c, a.acceptR, "接受并继续(A)", a.press == "accept", true)
	ui.Button(c, a.declineR, "拒绝(R)", a.press == "decline", true)
}

func (a *welcomeApp) Mouse(d *Desktop, w *Window, client ui.Rect, ev MouseEvent) {
	if ev.Kind == MouseWheel {
		before := a.scroll.Pos
		a.scroll.ScrollBy(-ev.Wheel * 3)
		if before != a.scroll.Pos {
			d.Invalidate()
		}
		return
	}
	if a.scrollR.Contains(ev.X, ev.Y) {
		if changed, _ := a.scroll.Mouse(ev, a.scrollR); changed {
			d.Invalidate()
		}
		return
	}
	switch ev.Kind {
	case MouseDown:
		if ev.Button != 1 {
			return
		}
		switch {
		case a.acceptR.Contains(ev.X, ev.Y):
			a.press = "accept"
		case a.declineR.Contains(ev.X, ev.Y):
			a.press = "decline"
		}
		d.Invalidate()
	case MouseUp:
		press := a.press
		a.press = ""
		if ev.Button != 1 {
			return
		}
		switch press {
		case "accept":
			if a.acceptR.Contains(ev.X, ev.Y) {
				a.accept()
				return
			}
		case "decline":
			if a.declineR.Contains(ev.X, ev.Y) {
				a.decline()
				return
			}
		}
		d.Invalidate()
	}
}

func (a *welcomeApp) Key(d *Desktop, w *Window, ev KeyEvent) bool {
	switch ev.Name {
	case "Enter":
		a.accept()
		return true
	case "Esc":
		a.decline()
		return true
	case "Up":
		a.scroll.ScrollBy(-1)
	case "Down":
		a.scroll.ScrollBy(1)
	case "PageUp":
		a.scroll.ScrollBy(-a.scroll.Visible)
	case "PageDown":
		a.scroll.ScrollBy(a.scroll.Visible)
	case "Home":
		a.scroll.Pos = 0
	case "End":
		a.scroll.Pos = a.scroll.Max()
	default:
		if ev.Rune == 'a' || ev.Rune == 'A' {
			a.accept()
			return true
		}
		if ev.Rune == 'r' || ev.Rune == 'R' {
			a.decline()
			return true
		}
		return false
	}
	d.Invalidate()
	return true
}

func (a *welcomeApp) accept() {
	a.d.eulaAccepted = true
	a.d.savePrefs()
	if a.win != nil {
		a.d.CloseWindowForced(a.win)
	}
	a.d.Notify("Windows 95 Linux", "欢迎！许可协议已接受。")
	n := NewNotepadApp(a.d, "")
	n.SetContent("/root/文档/欢迎使用.txt", welcomeText)
	nw := a.d.Open(n)
	n.win = nw
}

func (a *welcomeApp) decline() {
	if a.win != nil {
		a.d.CloseWindowForced(a.win)
	}
	a.d.MessageBox("许可协议",
		"您未接受许可协议。\n\n下次启动系统时会再次显示本协议；\n接受后即可正常使用全部功能。",
		IconInfo, []string{"确定"}, nil)
}

const apacheLicense = `Apache License
Version 2.0, January 2004
http://www.apache.org/licenses/

TERMS AND CONDITIONS FOR USE, REPRODUCTION, AND DISTRIBUTION

1. Definitions.

"License" shall mean the terms and conditions for use, reproduction, and distribution as defined by Sections 1 through 9 of this document.

"Licensor" shall mean the copyright owner or entity authorized by the copyright owner that is granting the License.

"Legal Entity" shall mean the union of the acting entity and all other entities that control, are controlled by, or are under common control with that entity. For the purposes of this definition, "control" means (i) the power, direct or indirect, to cause the direction or management of such entity, whether by contract or otherwise, or (ii) ownership of fifty percent (50%) or more of the outstanding shares, or (iii) beneficial ownership of such entity.

"You" (or "Your") shall mean an individual or Legal Entity exercising permissions granted by this License.

"Source" form shall mean the preferred form for making modifications, including but not limited to software source code, documentation source, and configuration files.

"Object" form shall mean any form resulting from mechanical transformation or translation of a Source form, including but not limited to compiled object code, generated documentation, and conversions to other media types.

"Work" shall mean the work of authorship, whether in Source or Object form, made available under the License, as indicated by a copyright notice that is included in or attached to the work (an example is provided in the Appendix below).

"Derivative Works" shall mean any work, whether in Source or Object form, that is based on (or derived from) the Work and for which the editorial revisions, annotations, elaborations, or other modifications represent, as a whole, an original work of authorship. For the purposes of this License, Derivative Works shall not include works that remain separable from, or merely link (or bind by name) to the interfaces of, the Work and Derivative Works thereof.

"Contribution" shall mean any work of authorship, including the original version of the Work and any modifications or additions to that Work or Derivative Works thereof, that is intentionally submitted to Licensor for inclusion in the Work by the copyright owner or by an individual or Legal Entity authorized to submit on behalf of the copyright owner. For the purposes of this definition, "submitted" means any form of electronic, verbal, or written communication sent to the Licensor or its representatives, including but not limited to communication on electronic mailing lists, source code control systems, and issue tracking systems that are managed by, or on behalf of, the Licensor for the purpose of discussing and improving the Work, but excluding communication that is conspicuously marked or otherwise designated in writing by the copyright owner as "Not a Contribution."

"Contributor" shall mean Licensor and any individual or Legal Entity on behalf of whom a Contribution has been received by Licensor and subsequently incorporated within the Work.

2. Grant of Copyright License. Subject to the terms and conditions of this License, each Contributor hereby grants to You a perpetual, worldwide, non-exclusive, no-charge, royalty-free, irrevocable copyright license to reproduce, prepare Derivative Works of, publicly display, publicly perform, sublicense, and distribute the Work and such Derivative Works in Source or Object form.

3. Grant of Patent License. Subject to the terms and conditions of this License, each Contributor hereby grants to You a perpetual, worldwide, non-exclusive, no-charge, royalty-free, irrevocable (except as stated in this section) patent license to make, have made, use, offer to sell, sell, import, and otherwise transfer the Work, where such license applies only to those patent claims licensable by such Contributor that are necessarily infringed by their Contribution(s) alone or by combination of their Contribution(s) with the Work to which such Contribution(s) was submitted. If You institute patent litigation against any entity (including a cross-claim or counterclaim in a lawsuit) alleging that the Work or a Contribution incorporated within the Work constitutes direct or contributory patent infringement, then any patent licenses granted to You under this License for that Work shall terminate as of the date such litigation is filed.

4. Redistribution. You may reproduce and distribute copies of the Work or Derivative Works thereof in any medium, with or without modifications, and in Source or Object form, provided that You meet the following conditions:

(a) You must give any other recipients of the Work or Derivative Works a copy of this License; and

(b) You must cause any modified files to carry prominent notices stating that You changed the files; and

(c) You must retain, in the Source form of any Derivative Works that You distribute, all copyright, patent, trademark, and attribution notices from the Source form of the Work, excluding those notices that do not pertain to any part of the Derivative Works; and

(d) If the Work includes a "NOTICE" text file as part of its distribution, then any Derivative Works that You distribute must include a readable copy of the attribution notices contained within such NOTICE file, excluding those notices that do not pertain to any part of the Derivative Works, in at least one of the following places: within a NOTICE text file distributed as part of the Derivative Works; within the Source form or documentation, if provided along with the Derivative Works; or, within a display generated by the Derivative Works, if and wherever such third-party notices normally appear. The contents of the NOTICE file are for informational purposes only and do not modify the License. You may add Your own attribution notices within Derivative Works that You distribute, alongside or as an addendum to the NOTICE text from the Work, provided that such additional attribution notices cannot be construed as modifying the License.

You may add Your own copyright statement to Your modifications and may provide additional or different license terms and conditions for use, reproduction, or distribution of Your modifications, or for any such Derivative Works as a whole, provided Your use, reproduction, and distribution of the Work otherwise complies with the conditions stated in this License.

5. Submission of Contributions. Unless You explicitly state otherwise, any Contribution intentionally submitted for inclusion in the Work by You to the Licensor shall be under the terms and conditions of this License, without any additional terms or conditions. Notwithstanding the above, nothing herein shall supersede or modify the terms of any separate license agreement you may have executed with Licensor regarding such Contributions.

6. Trademarks. This License does not grant permission to use the trade names, trademarks, service marks, or product names of the Licensor, except as required for reasonable and customary use in describing the origin of the Work and reproducing the content of the NOTICE file.

7. Disclaimer of Warranty. Unless required by applicable law or agreed to in writing, Licensor provides the Work (and each Contributor provides its Contributions) on an "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied, including, without limitation, any warranties or conditions of TITLE, NON-INFRINGEMENT, MERCHANTABILITY, or FITNESS FOR A PARTICULAR PURPOSE. You are solely responsible for determining the appropriateness of using or redistributing the Work and assume any risks associated with Your exercise of permissions under this License.

8. Limitation of Liability. In no event and under no legal theory, whether in tort (including negligence), contract, or otherwise, unless required by applicable law (such as deliberate and grossly negligent acts) or agreed to in writing, shall any Contributor be liable to You for damages, including any direct, indirect, special, incidental, or consequential damages of any character arising as a result of this License or out of the use or inability to use the Work (including but not limited to damages for loss of goodwill, work stoppage, computer failure or malfunction, or any and all other commercial damages or losses), even if such Contributor has been advised of the possibility of such damages.

9. Accepting Warranty or Additional Liability. While redistributing the Work or Derivative Works thereof, You may choose to offer, and charge a fee for, acceptance of support, warranty, indemnity, or other liability obligations and/or rights consistent with this License. However, in accepting such obligations, You may act only on Your own behalf and on Your sole responsibility, not on behalf of any other Contributor, and only if You agree to indemnify, defend, and hold each Contributor harmless for any liability incurred by, or claims asserted against, such Contributor by reason of your accepting any such warranty or additional liability.

END OF TERMS AND CONDITIONS

APPENDIX: How to apply the Apache License to your work.

To apply the Apache License to your work, attach the following boilerplate notice, with the fields enclosed by brackets "[]" replaced with your own identifying information. (Don't include the brackets!) The text should be enclosed in the appropriate comment syntax for the file format. We also recommend that a file or class name and description of purpose be included on the same "printed page" as the copyright notice for easier identification within third-party archives.

Copyright [yyyy] [name of copyright owner]

Licensed under the Apache License, Version 2.0 (the "License"); you may not use this file except in compliance with the License. You may obtain a copy of the License at

http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software distributed under the License is distributed on an "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied. See the License for the specific language governing permissions and limitations under the License.`
