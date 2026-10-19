# GitHub 发布与修订包流程

适用于 `sunqing37/loghelp` 的正式版本及同版本修订。发布、推送、替换公开附件需用户授权；同一会话已有明确授权时沿用，不重复询问。只分析或编写文档不会自动发布。

## 1. 确认版本和源码

1. 查看 `git status --short`、分支、远端和现有 Release；确认目标仓库及实际发布范围。
2. 新产品版本更新根 `VERSION` 并确定 `REVISION`；同版本发布新二进制必须增加根 `REVISION`，例如 `0.0.1 r2 → 0.0.1 r3`。同时核对应用内嵌信息和 UI 显示。
3. 更新 `CHANGELOG.md`、`docs/releases/<版本>.md` 和必要的手动验收记录。内容与最终产物一致。
4. 按提交规范提交并按授权推送；记录源码 commit、VERSION、REVISION，避免包、源码和发布说明错位。

## 2. 构建并准备附件

取得完整桌面构建授权后执行一次；源码及指纹未变且已成功构建时直接复用。

```powershell
python -X utf8 scripts/build.py --desktop
python -X utf8 .agent/scripts/package-release.py --all
```

Windows 主机可加 `--setup`，在 ZIP 之外为 Windows 目标生成当前用户安装包；仅打包 Windows amd64 使用 `python -X utf8 .agent/scripts/package-release.py --setup`，六平台使用 `--all --setup`。要求 Inno Setup 6.4+（可用 `--iscc` 指定），不重新编译 Go。Setup 固定使用用户可写目录；x64 复用 ZIP 自动更新，ARM64 手动升级，因此发布 Setup 时必须同时上传同版本、架构、修订的 ZIP 和校验文件。

Setup 名称为 `loghelp-<版本>-windows-<架构>[-rN]-setup.exe`，并合并到同一个包外 `SHA256SUMS[-rN].txt`。同名 JSON 是本地打包审计记录，记录脚本、编译器、输入和产物哈希，无需上传。已有不同内容的 Setup 不覆盖，正式发布需递增修订。`--local` 仅用于未提交改动的本地验收，产物进入 `artifacts/packages/<版本>/` 且元数据标记本地预览，不允许作为正式 Release 附件。

安装包手动验收包括：当前用户安装与快捷方式、同版修复/新版覆盖、拒绝旧修订、安装版 ZIP 更新及系统版本登记、运行期间安装/卸载拦截、默认保留数据、确认删除数据和静默卸载保留数据。清理验证使用隔离用户环境；不得删除开发者真实设置或录制目录。

打包脚本检查目标、版本、构建指纹和二进制哈希，不重新编译。发布目录为 `dist/<版本>/`：

多平台发布逐一执行 `scripts/build.py --desktop --cross-desktop --target-os <os> --arch <arch>`，串行链接 Windows/darwin/Linux 的 amd64/arm64。`--all` 核对六个成功构建记录后打包；也可用 `--target-os` 和 `--arch` 单独打包（默认 Windows amd64）。交叉链接与目标系统运行验收必须分别报告。

打包要求工作区干净，包内源码 commit 必须对应实际版本。若用户要求发布后推送主分支，可先本地提交并推送发布标签，发布完成后再推送主分支；标签与包始终指向同一源码。

- 原始 `r0` 包：`loghelp-<版本>-<os>-<arch>.zip`。
- 修订 `rN` 包：`loghelp-<版本>-<os>-<arch>-r<N>.zip`。例如 `loghelp-0.0.1-windows-amd64-r3.zip`。
- 包外校验文件：原始包 `SHA256SUMS.txt`，修订包 `SHA256SUMS-r<N>.txt`，内容记录 ZIP 文件名和 SHA-256。
- ZIP 内包含平台可执行程序（Windows 为 EXE）、使用说明、版本/修订元数据及程序校验文件；不要捆绑本地设置、日志、录制、凭据或开发环境。

核对 EXE 内嵌版本、修订、Windows 图标和手动验收记录。检查包外校验文件中的文件名与 ZIP 完全一致。

## 3. 标签与 Release

- 新版本创建 `v<版本>` 标签，指向已提交的发布源码，按授权推送后创建正式 Release；确认不是 Draft / Pre-release，并标记 Latest。
- 已公开的标签默认保持不变，禁止例行 `git tag -f` 或强推。修订通过更高 `rN` 附件发布；说明中记录对应源码 commit。
- 同一正式 Release 的新修订可创建独立源码标签（例如 `v0.0.1-r3`）用于回溯，Release 仍绑定正式标签 `v0.0.1`。在发布说明中链接修订源码标签；不要将该源码标签另建为 Latest 正式 Release，因为检查接口的产品版本来自正式标签。
- 如果用户明确授权同一版本标签也随修订更新，先记录远端旧 tag object / commit，再用明确旧值的 lease 校验更新，不无条件覆盖其他会话改动。记录移动标签事实与新源码 commit，确保发布来源可追溯。
- 更新现有 `v0.0.1` Release 时，上传新的修订 ZIP 和校验文件，更新正文的推荐下载与修订说明。保留原始包与历史修订附件，供回溯；不要默默把旧文件名换成新内容。
- 上传失败或结果不明确时先检查附件列表，避免重复上传。相同附件名已经存在时先比对大小和哈希；不同内容的覆盖/删除不得擅自进行。

## 4. 上传方式

优先可用 GitHub API / CLI，凭据不要打印或写入仓库；没有适当工具时使用已登录的内置浏览器。若必须登录，等待用户自行完成登录，不索取密码或 token。

有可用且已认证的 `gh` 时，上传已准备的两个文件（将示例 `r3` 替换为实际修订）：

```powershell
gh release view v0.0.1 --repo sunqing37/loghelp
gh release upload v0.0.1 dist/0.0.1/loghelp-0.0.1-windows-amd64-r3.zip dist/0.0.1/SHA256SUMS-r3.txt --repo sunqing37/loghelp
gh release edit v0.0.1 --repo sunqing37/loghelp --notes-file docs/releases/0.0.1.md --latest
```

不默认使用 `--clobber`。多行正文用 UTF-8 文件传递。浏览器方式打开 `https://github.com/sunqing37/loghelp/releases/edit/v0.0.1`，上传实际产物和校验文件，确认上传结束再保存，最后读取已公开的 Release 核对。

## 5. 发布后核验

1. 从 Release API / 页面重新读取正式状态、Latest、附件名称及大小，确认下载 URL 可访问。
2. 比对 GitHub 提供的 SHA-256 digest 与本地 ZIP；若没有 digest，下载到校验临时目录后计算 SHA-256。再核对公开的校验文件内容，不仅依赖上传成功提示。
3. 应用检查更新应选到平台/架构匹配的最高修订，与包内校验文件一致；用更低修订的验收副本检查提示和替换路径，记录实际结果。
4. `r2` 程序只比较产品版本，无法因仍为 `0.0.1` 识别 `r3`。需手动下载替换一次，随后修订识别版才能提示 `r4` 等同版本更新；发布说明和交付说明中必须明确这一限制。
5. 最终交付提供 Release、修订包及校验文件的直接链接，说明实际完成的验证。源码无需修改时禁止为了发布后 review 重复构建。

## 6. 只读手动查询

程序提供 `--version` 和 `--check-update`。查询命令不启动界面、不下载或安装附件；后者可以显式指定用于比较的旧版本/修订，方便核对正式 API 返回结果。省略比较参数时使用程序内嵌值。

Windows GUI 程序建议通过 Python 子进程收集输出：

```powershell
python -X utf8 -c "import subprocess; print(subprocess.check_output(['bin/loghelp.exe','--version'],text=True,encoding='utf-8'))"
python -X utf8 -c "import subprocess; print(subprocess.check_output(['bin/loghelp.exe','--check-update','--current-revision','2'],text=True,encoding='utf-8'))"
```

人工核对结果中的 `current_revision`、`release.Revision`、`release.Newer`、附件 URL、大小和 digest。较低修订应提示更新，相同或更高修订不应提示降级；这属于手动只读检查，不新增测试文件。
