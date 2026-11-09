import Cocoa

let fm = FileManager.default
let defaults = UserDefaults.standard

final class TopDownView: NSView { override var isFlipped: Bool { true } }

final class Controller: NSObject, NSApplicationDelegate {
    var root: URL!
    let runtime = Bundle.main.resourceURL!.appendingPathComponent("runtime")
    var item: NSStatusItem!
    var trayStatus: NSMenuItem!
    var trayWeb: NSMenuItem!
    var trayQuit: NSMenuItem!
    var window: NSWindow!
    var text: NSTextView!
    var child: Process?
    var timer: Timer?
    var quitting = false
    var quittingAt: Date?
    var didShowFailure = false
    var selected = "backend"
    var reading = false
    var serviceButtons: [String:NSButton] = [:]
    var heading: NSTextField!
    var subtitle: NSTextField!
    var serviceState: NSTextField!
    var endpoint: NSTextField!
    var address: NSTextField!
    var detail: NSTextField!
    var summary: NSTextField!
    var version: NSTextField!
    var startButton: NSButton!
    var stopButton: NSButton!
    var webButton: NSButton!
    var currentLog: URL?
    var currentURL = ""
    let muted = NSColor(calibratedWhite:0.46,alpha:1)
    let green = NSColor(red:0.27,green:0.49,blue:0.08,alpha:1)

    func label(_ value:String,_ x:CGFloat,_ y:CGFloat,_ w:CGFloat,_ h:CGFloat,_ size:CGFloat = 14,_ bold:Bool = false,_ color:NSColor = .labelColor) -> NSTextField {
        let v=NSTextField(labelWithString:value);v.frame=NSRect(x:x,y:y,width:w,height:h)
        v.font=NSFont.systemFont(ofSize:size,weight:bold ? .semibold : .regular);v.textColor=color
        v.lineBreakMode = .byTruncatingTail;window.contentView!.addSubview(v);return v
    }
    func button(_ title:String,_ action:Selector,_ x:CGFloat,_ y:CGFloat,_ w:CGFloat,_ h:CGFloat = 36) -> NSButton {
        let b=NSButton(title:title,target:self,action:action);b.frame=NSRect(x:x,y:y,width:w,height:h)
        b.bezelStyle = .rounded;b.font=NSFont.systemFont(ofSize:14,weight:.medium);window.contentView!.addSubview(b);return b
    }
    func createManager() {
        window=NSWindow(contentRect:NSRect(x:0,y:0,width:980,height:700),styleMask:[.titled,.closable,.miniaturizable],backing:.buffered,defer:false)
        window.title="Tier0 Edge · 服务管理";window.isReleasedWhenClosed=false
        window.appearance=NSAppearance(named:.aqua);window.backgroundColor = .white
        window.contentView=TopDownView(frame:NSRect(x:0,y:0,width:980,height:700))
        let sidebar=NSView(frame:NSRect(x:0,y:0,width:240,height:636));sidebar.wantsLayer=true;sidebar.layer?.backgroundColor=NSColor(calibratedWhite:0.97,alpha:1).cgColor;window.contentView!.addSubview(sidebar)
        let logo=NSImageView(frame:NSRect(x:24,y:28,width:32,height:32));logo.image=NSImage(contentsOf:Bundle.main.resourceURL!.appendingPathComponent("favicon.ico"));window.contentView!.addSubview(logo)
        _=label("Tier0 Edge",68,30,160,30,21,true)
        _=label("本机服务",24,100,190,25,14,false,muted)
        let ids=["backend","postgres","nats","mqtt","opcua","modbus","executor","fleet","apps"]
        for (i,id) in ids.enumerated() {
            let b=button(id,#selector(selectService(_:)),14,139+CGFloat(i)*44,212,38);b.identifier=NSUserInterfaceItemIdentifier(id)
            b.isBordered=false;b.alignment = .left;b.wantsLayer=true;b.layer?.cornerRadius=8;serviceButtons[id]=b
        }
        version=label("",24,601,190,24,13,false,muted)
        heading=label("正在启动…",276,34,470,40,28,true)
        subtitle=label("",276,81,450,25,15,false,muted)
        webButton=button("打开平台",#selector(openWeb),810,38,140,40);webButton.bezelColor = .black;webButton.contentTintColor = .white
        serviceState=label("等待启动",276,123,650,30,15,true,green)
        endpoint=label("",276,165,650,27,15)
        address=label("",276,204,510,27,15,false,muted)
        _=button("复制地址",#selector(copyAddress),824,198,126,34)
        detail=label("",276,242,674,48,14,false,muted);detail.maximumNumberOfLines=2;detail.lineBreakMode = .byWordWrapping
        _=label("最近日志",276,303,300,30,20,true)
        _=button("打开日志文件",#selector(serviceLog),806,300,144,34)
        let scroll=NSScrollView(frame:NSRect(x:276,y:345,width:674,height:212));scroll.hasVerticalScroller=true;scroll.borderType = .noBorder;scroll.wantsLayer=true;scroll.layer?.cornerRadius=8
        text=NSTextView(frame:scroll.bounds);text.isEditable=false;text.isSelectable=true;text.font=NSFont.monospacedSystemFont(ofSize:12,weight:.regular);text.backgroundColor=NSColor(calibratedWhite:0.975,alpha:1);text.textColor = .labelColor;text.textContainerInset=NSSize(width:14,height:14);text.autoresizingMask=[.width];text.textContainer?.widthTracksTextView=true
        scroll.documentView=text;window.contentView!.addSubview(scroll)
        _=button("数据目录",#selector(dataDirectory),276,581,118,34)
        _=button("日志文件夹",#selector(logs),406,581,118,34)
        _=label("关闭窗口后继续在后台运行",276,649,320,24,13,false,muted)
        summary=label("正在启动",24,648,240,26,14,true,green)
        startButton=button("启动全部服务",#selector(start),626,641,142,38)
        stopButton=button("停止全部服务",#selector(stop),780,641,142,38)
        _=button("…",#selector(more(_:)),932,641,32,38)
        window.center();showProgress()
    }
    @objc func selectService(_ sender:NSButton){selected=sender.identifier!.rawValue;refresh()}
    @objc func copyAddress(){NSPasteboard.general.clearContents();NSPasteboard.general.setString(currentURL,forType:.string)}
    @objc func serviceLog(){if let url=currentLog {if !NSWorkspace.shared.open(url){showError("日志文件尚未生成，请启动服务后重试。")}}}
    @objc func more(_ sender:NSButton){let menu=NSMenu();for (title,action) in [("隐藏窗口",#selector(hideWindow)),("退出并停止服务",#selector(quit))]{let entry=NSMenuItem(title:title,action:action,keyEquivalent:"");entry.target=self;menu.addItem(entry)};menu.popUp(positioning:nil,at:NSPoint(x:0,y:sender.bounds.height),in:sender)}
    @objc func hideWindow(){window.orderOut(nil)}
    func stateLabel(_ state:String)->String { ["pending":"等待启动","starting":"启动中","running":"进程运行","ready":"已就绪","stopping":"停止中","stopped":"已停止","failed":"运行异常","unknown":"状态未知"][state] ?? state }

    func createTray() {
        item=NSStatusBar.system.statusItem(withLength:NSStatusItem.squareLength)
        let icon=NSImage(contentsOf:Bundle.main.resourceURL!.appendingPathComponent("favicon.ico"));icon?.size=NSSize(width:18,height:18)
        item.button?.image=icon
        let menu=NSMenu();menu.autoenablesItems=false
        trayStatus=NSMenuItem(title:"启动中",action:nil,keyEquivalent:"");trayStatus.isEnabled=false
        menu.addItem(trayStatus);menu.addItem(.separator())
        trayWeb=NSMenuItem(title:"打开 Tier0 Edge",action:#selector(openWeb),keyEquivalent:"")
        trayWeb.target=self;menu.addItem(trayWeb)
        let manager=NSMenuItem(title:"服务管理…",action:#selector(showProgress),keyEquivalent:"")
        manager.target=self;menu.addItem(manager);menu.addItem(.separator())
        trayQuit=NSMenuItem(title:"退出并停止服务",action:#selector(quit),keyEquivalent:"")
        trayQuit.target=self;menu.addItem(trayQuit)
        item.menu=menu;updateTray("启动中")
    }
    func updateTray(_ status:String,canOpen:Bool = false) {
        trayStatus.title=quitting ? "正在退出…":status
        trayWeb.isEnabled=canOpen && !quitting;trayQuit.isEnabled = !quitting
        webButton?.isEnabled=trayWeb.isEnabled
        item.button?.toolTip="Tier0 Edge · \(trayStatus.title)"
    }
    func showError(_ message:String) { let a=NSAlert();a.messageText="Tier0 Edge";a.informativeText=message;a.runModal() }
    func applicationDidFinishLaunching(_ notification: Notification) {
        if Bundle.main.bundleURL.path.hasPrefix("/Volumes/") {
            showError("请先将 Tier0 Edge.app 拖到 Applications，然后从 Applications 打开。")
            NSApp.terminate(nil);return
        }
        if let index=CommandLine.arguments.firstIndex(of:"--data-root"),index+1<CommandLine.arguments.count {
            root=URL(fileURLWithPath:CommandLine.arguments[index+1])
        } else if let saved=defaults.string(forKey:"dataDirectory") {
            root=URL(fileURLWithPath:saved)
        } else {
            let standard=fm.homeDirectoryForCurrentUser.appendingPathComponent("Library/Application Support/Tier0 Edge")
            let alert=NSAlert();alert.messageText="Tier0 Edge · 首次启动"
            alert.informativeText="程序保存在 Applications，数据库、账号、App、日志与 Fleet 身份保存在独立数据目录。更新应用不会替换数据。\n\n默认：\(standard.path)"
            alert.addButton(withTitle:"使用默认目录");alert.addButton(withTitle:"选择其他目录");alert.addButton(withTitle:"取消")
            switch alert.runModal() {
            case .alertFirstButtonReturn: root=standard
            case .alertSecondButtonReturn:
                let panel=NSOpenPanel();panel.title="选择 Tier0 Edge 数据目录（空目录或已有兼容数据目录）"
                panel.canChooseFiles=false;panel.canChooseDirectories=true;panel.canCreateDirectories=true
                guard panel.runModal() == .OK,let url=panel.url else {NSApp.terminate(nil);return};root=url
            default: NSApp.terminate(nil);return
            }
        }
        do {
            let path=root.standardizedFileURL.path
            guard !path.contains(".app/") && !path.hasSuffix(".app") else {throw NSError(domain:"Tier0",code:1,userInfo:[NSLocalizedDescriptionKey:"数据目录不能位于应用包中"])}
            try fm.createDirectory(at:root,withIntermediateDirectories:true,attributes:[.posixPermissions:0o700])
            root=root.resolvingSymlinksInPath()
            defaults.set(root.path,forKey:"dataDirectory")
        } catch {showError(error.localizedDescription);NSApp.terminate(nil);return}
        createTray()
        createManager()
        timer=Timer.scheduledTimer(withTimeInterval:1.5,repeats:true){[weak self] _ in self?.refresh()}
        start()
    }
    @objc func showProgress(){window.makeKeyAndOrderFront(nil);NSApp.activate(ignoringOtherApps:true)}
    @objc func openWeb(){
        var port="18080"
        if let d=try? Data(contentsOf:root.appendingPathComponent("state/environment.json")),let s=(try? JSONSerialization.jsonObject(with:d)) as? [String:String]{port=s["NATIVE_HTTP_PORT"] ?? port}
        if let url=URL(string:"http://127.0.0.1:\(port)"){NSWorkspace.shared.open(url)}
    }
    @objc func logs(){NSWorkspace.shared.open(root.appendingPathComponent("logs"))}
    @objc func dataDirectory(){NSWorkspace.shared.open(root)}
    @objc func start(){
        if child?.isRunning == true || quitting{return}
        didShowFailure=false
        updateTray("启动中")
        do{
            try fm.createDirectory(at:root.appendingPathComponent("logs"),withIntermediateDirectories:true)
            let path=root.appendingPathComponent("logs/launcher.log").path
            if !fm.fileExists(atPath:path){fm.createFile(atPath:path,contents:nil)}
            let log=try FileHandle(forWritingTo:URL(fileURLWithPath:path));try log.seekToEnd()
            let p=Process();p.executableURL=runtime.appendingPathComponent("native-launcher")
            p.arguments=["--root",root.path,"--no-browser"];p.standardOutput=log;p.standardError=log
            try p.run();child=p;try log.close();startButton.isEnabled=false;summary.stringValue="正在启动…"
        }catch{updateTray("运行异常");showError(error.localizedDescription)}
    }
    @objc func stop(){guard child?.isRunning == true else{return};do{try Data("stop".utf8).write(to:root.appendingPathComponent("stop.request"))}catch{showError(error.localizedDescription)}}
    @objc func quit(){if child?.isRunning != true{NSApp.terminate(nil);return};quitting=true;quittingAt=Date();updateTray("正在退出…");stop()}
    func applicationShouldTerminate(_ sender:NSApplication)->NSApplication.TerminateReply {
        if child?.isRunning == true {if !quitting{quit()};return .terminateCancel};return .terminateNow
    }
    func refresh(){
        if quitting && child?.isRunning != true {timer?.invalidate();NSApp.terminate(nil);return}
        if let p=child,!p.isRunning,p.terminationStatus != 0,!didShowFailure {
            didShowFailure=true;showProgress()
        }
        if quitting,let since=quittingAt,Date().timeIntervalSince(since)>90{quitting=false;showError("停止超时，请查看日志。不要替换正在运行的应用。")}
        guard !reading else{return};reading=true
        let selectedID=selected, dataRoot=root.path, executable=runtime.appendingPathComponent("native-launcher")
        DispatchQueue.global(qos:.utility).async { [weak self] in
            let p=Process();let pipe=Pipe();p.executableURL=executable;p.arguments=["--root",dataRoot,"--status-json","--service",selectedID];p.standardOutput=pipe;p.standardError=FileHandle.nullDevice
            var result:[String:Any]?
            do {try p.run();let data=pipe.fileHandleForReading.readDataToEndOfFile();p.waitUntilExit();if p.terminationStatus==0 {result=(try? JSONSerialization.jsonObject(with:data)) as? [String:Any]}} catch {}
            let snapshot=result
            DispatchQueue.main.async {
                guard let self=self else{return};self.reading=false
                guard self.selected==selectedID else {self.refresh();return}
                if let snapshot=snapshot {self.render(snapshot)}else {
                    self.summary.stringValue="状态读取失败";self.detail.stringValue="请查看启动器日志后重试。"
                    self.startButton.isEnabled = self.child?.isRunning != true && !self.quitting
                    self.stopButton.isEnabled = self.child?.isRunning == true && !self.quitting
                    self.updateTray("状态读取失败")
                }
            }
        }
    }
    func render(_ p:[String:Any]) {
        var phase=p["phase"] as? String ?? "unknown"
        let active=child?.isRunning == true
        let sameProcess=(p["pid"] as? Int)==Int(child?.processIdentifier ?? -1)
        if didShowFailure {phase="failed"}else if active && !sameProcess {phase="starting"}else if !active {phase="stopped"}
        let services=p["services"] as? [[String:Any]] ?? []
        var count=0
        for service in services {
            let id=service["id"] as? String ?? "",state=service["state"] as? String ?? "unknown"
            let shown=active && sameProcess ? state : (active ? "pending":"stopped")
            if shown=="ready" || shown=="running" {count+=1}
            let name=["backend":"Tier0 Core","postgres":"PostgreSQL","apps":"App Runtime"][id] ?? (service["label"] as? String ?? id)
            if let b=serviceButtons[id] {
                b.title="  \(name)";b.setAccessibilityLabel("\(name) · \(stateLabel(shown))");b.toolTip=stateLabel(shown)
                b.font=NSFont.systemFont(ofSize:14,weight:id==selected ? .semibold:.regular)
                let color=shown=="ready" || shown=="running" ? green:(shown=="failed" ? .systemRed:muted)
                b.image=NSImage(systemSymbolName:"circle.fill",accessibilityDescription:stateLabel(shown))?.withSymbolConfiguration(NSImage.SymbolConfiguration(pointSize:8,weight:.regular).applying(NSImage.SymbolConfiguration(paletteColors:[color])))
                b.imagePosition = .imageLeading
                b.layer?.backgroundColor=(id==selected ? NSColor(red:0.92,green:0.97,blue:0.82,alpha:1):.clear).cgColor
            }
        }
        let service=p["selected"] as? [String:Any] ?? [:]
        heading.stringValue=service["label"] as? String ?? "服务状态"
        subtitle.stringValue=p["description"] as? String ?? ""
        let state=active && sameProcess ? (service["state"] as? String ?? "unknown") : (active ? "pending":"stopped")
        serviceState.stringValue=stateLabel(state);serviceState.textColor=state=="ready" || state=="running" ? green:muted
        endpoint.stringValue="服务端点    \(p["endpoint"] as? String ?? "—")"
        currentURL=p["url"] as? String ?? "";address.stringValue="平台地址    \(currentURL)"
        detail.stringValue=phase=="ready" ? (service["message"] as? String ?? ""):(p["message"] as? String ?? "")
        if let error=p["error"] as? String,!error.isEmpty {detail.stringValue=error}
        if didShowFailure {detail.stringValue="启动或升级失败。原数据与备份已保留，请查看 launcher.log 和 upgrade-pending.json。"}
        let log=p["log"] as? String ?? "暂无日志"
        if text.string != log {text.string=log;text.scrollToEndOfDocument(nil)}
        if let file=p["logFile"] as? String {currentLog=URL(fileURLWithPath:file)}
        version.stringValue="v\(p["version"] as? String ?? "dev")"
        summary.stringValue=phase=="ready" ? "\(count) / \(services.count) 服务运行中":stateLabel(phase)
        summary.textColor=phase=="failed" ? .systemRed:green
        startButton.isEnabled = !active && !quitting;stopButton.isEnabled=active && phase != "stopping" && !quitting
        let backendReady=services.contains {($0["id"] as? String)=="backend" && ($0["state"] as? String)=="ready"}
        let failed=phase=="ready" && services.contains {($0["state"] as? String)=="failed"}
        updateTray(stateLabel(failed ? "failed":phase),canOpen:active && sameProcess && phase=="ready" && backendReady)
    }
}
let app=NSApplication.shared
app.setActivationPolicy(.accessory)
let controller=Controller();app.delegate=controller;app.run()
