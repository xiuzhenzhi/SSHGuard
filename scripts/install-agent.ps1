#Requires -Version 5.1
#Requires -RunAsAdministrator
param(
    [Parameter(Mandatory=$true)][string]$Endpoint,
    [ValidatePattern('^[a-f0-9]{64}$')][string]$EnrollmentToken,
    [switch]$Update,
    [switch]$AllowInsecureHttp
)
$ReleaseVersion = '0.4.0'
$ErrorActionPreference = 'Stop'
$ProgressPreference = 'SilentlyContinue'
Write-Output "`nSSHGuard Agent · 安装与更新"
Write-Output '[1/4] 检查系统与依赖'
if ($Update -and $EnrollmentToken) { throw '更新与首次安装参数不能同时使用。' }
if (-not $Update -and -not $EnrollmentToken) { throw '缺少安装凭证，请从控制台复制完整安装命令。' }
$Endpoint = $Endpoint.TrimEnd('/')
$origin = [Uri]$Endpoint
if (($origin.Scheme -ne 'https' -and -not ($origin.Scheme -eq 'http' -and $AllowInsecureHttp)) -or -not $origin.Host -or $origin.UserInfo -or $origin.Query -or $origin.Fragment -or $origin.AbsolutePath -ne '/' -or $origin.Port -lt 1 -or $origin.Port -gt 65535) { throw '主控地址格式错误；HTTP 地址需加 -AllowInsecureHttp。' }
if ($origin.Scheme -eq 'http') { Write-Output '[提示] 当前使用 HTTP，请仅在可信网络使用。' }
[Net.ServicePointManager]::SecurityProtocol = [Net.SecurityProtocolType]::Tls12
$targetVersion = if ($env:SSHGUARD_EXPECTED_VERSION) { $env:SSHGUARD_EXPECTED_VERSION } else { $ReleaseVersion }
if ($targetVersion.Length -gt 32 -or $targetVersion -notmatch '^[0-9]+\.[0-9]+\.[0-9]+(-[A-Za-z0-9.-]+)?$') { throw '版本号无效，请重新复制控制台安装命令。' }
$artifactBase = "https://github.com/xiuzhenzhi/SSHGuard/releases/download/agent-v$targetVersion"
$artifactProtocol = '=https'
if ($env:SSHGUARD_TEST_DOWNLOADS -eq 'true') {
    if ($env:GITHUB_ACTIONS -ne 'true' -or $origin.Host -notin @('localhost','127.0.0.1','guard.sshguard.test')) { throw 'Test download source requires the disposable CI environment.' }
    $artifactBase = "$Endpoint/downloads"
    if ($origin.Scheme -eq 'http') { $artifactProtocol = '=http' }
}
$curlExe = Join-Path ([Environment]::SystemDirectory) 'curl.exe'
if (-not (Test-Path -LiteralPath $curlExe)) { throw '缺少系统 curl.exe，需要 Windows 10/11 或 Server 2019 及以上版本。' }
function Get-ReleaseAsset([string]$Name,[string]$OutFile,[int]$MaxBytes,[int]$Timeout) {
    & $curlExe --proto $artifactProtocol --proto-redir '=https' --tlsv1.2 --connect-timeout 15 --max-time $Timeout --max-redirs 5 --max-filesize $MaxBytes -fLsS "$artifactBase/$Name" -o $OutFile
    if ($LASTEXITCODE -ne 0) { throw '下载失败，请检查 GitHub 连接后重试；原服务未替换。' }
}
$osArchitecture = $env:PROCESSOR_ARCHITEW6432
if (-not $osArchitecture) { $osArchitecture = $env:PROCESSOR_ARCHITECTURE }
switch ($osArchitecture) { 'AMD64' { $arch='amd64' } 'ARM64' { $arch='arm64' } default { throw '仅支持 Windows amd64 / arm64。' } }
$installDir = Join-Path $env:ProgramData 'SSHGuard\Agent'
$configDir = $installDir
$scriptsDir = Join-Path $installDir 'sshguard'
$exe = Join-Path $installDir 'sshguard-agent.exe'
$config = Join-Path $configDir 'agent.json'

if ((Test-Path -LiteralPath (Join-Path $env:ProgramFiles 'SSHGuardAgent\sshguard-agent.exe')) -or (Test-Path -LiteralPath (Join-Path $env:ProgramData 'SSHGuard\agent.json'))) {
    throw '检测到旧目录安装，请先使用旧版卸载脚本卸载，再重新安装。此安装程序不迁移旧数据。'
}

function Assert-NoReparsePoint([string]$Path) {
    $current = [IO.Path]::GetFullPath($Path)
    while ($current) {
        $item = Get-Item -LiteralPath $current -Force -ErrorAction SilentlyContinue
        if ($item -and ($item.Attributes -band [IO.FileAttributes]::ReparsePoint)) { throw "Refusing reparse point: $current" }
        $current = Split-Path -Parent $current
    }
}
function Protect-Directory([string]$Path) {
    Assert-NoReparsePoint $Path
    New-Item -ItemType Directory -Force -Path $Path | Out-Null
    $acl = New-Object Security.AccessControl.DirectorySecurity
    $acl.SetAccessRuleProtection($true, $false)
    $admins = New-Object Security.Principal.SecurityIdentifier('S-1-5-32-544')
    $acl.SetOwner($admins)
    foreach ($sid in @('S-1-5-32-544','S-1-5-18','S-1-5-19')) {
        $rights = if ($sid -eq 'S-1-5-19') { 'ReadAndExecute' } else { 'FullControl' }
        $identity = New-Object Security.Principal.SecurityIdentifier($sid)
        $rule = New-Object Security.AccessControl.FileSystemAccessRule($identity,$rights,'ContainerInherit,ObjectInherit','None','Allow')
        $acl.AddAccessRule($rule)
    }
    Set-Acl -LiteralPath $Path -AclObject $acl
}
function Protect-File([string]$Path) {
    $acl = New-Object Security.AccessControl.FileSecurity
    $acl.SetAccessRuleProtection($true,$false)
    $acl.SetOwner((New-Object Security.Principal.SecurityIdentifier('S-1-5-32-544')))
    foreach ($sid in @('S-1-5-32-544','S-1-5-18','S-1-5-19')) {
        $rights = if ($sid -eq 'S-1-5-19') { 'ReadAndExecute' } else { 'FullControl' }
        $identity = New-Object Security.Principal.SecurityIdentifier($sid)
        $acl.AddAccessRule((New-Object Security.AccessControl.FileSystemAccessRule($identity,$rights,'Allow')))
    }
    Set-Acl -LiteralPath $Path -AclObject $acl
}
function Initialize-CollectorAccess {
    $state = Join-Path $configDir 'sshguard-agent'
    Assert-NoReparsePoint $state
    New-Item -ItemType Directory -Force -Path $state | Out-Null
    $serviceIdentity = (New-Object Security.Principal.NTAccount('NT SERVICE\SSHGuardAgent')).Translate([Security.Principal.SecurityIdentifier])
    $acl = New-Object Security.AccessControl.DirectorySecurity
    $acl.SetAccessRuleProtection($true,$false)
    $acl.SetOwner((New-Object Security.Principal.SecurityIdentifier('S-1-5-32-544')))
    foreach ($sid in @('S-1-5-32-544','S-1-5-18')) {
        $identity = New-Object Security.Principal.SecurityIdentifier($sid)
        $acl.AddAccessRule((New-Object Security.AccessControl.FileSystemAccessRule($identity,'FullControl','ContainerInherit,ObjectInherit','None','Allow')))
    }
    $acl.AddAccessRule((New-Object Security.AccessControl.FileSystemAccessRule($serviceIdentity,'Modify','ContainerInherit,ObjectInherit','None','Allow')))
    Set-Acl -LiteralPath $state -AclObject $acl
    # Grant channel read permission only to this service SID.
    foreach ($channelName in @('OpenSSH/Operational', 'Security', 'Application')) {
    $channel = $null
    try {
        $channel = New-Object System.Diagnostics.Eventing.Reader.EventLogConfiguration($channelName)
        $descriptor = New-Object Security.AccessControl.RawSecurityDescriptor($channel.SecurityDescriptor)
        $found = $false
        foreach ($ace in $descriptor.DiscretionaryAcl) {
            if ($ace.SecurityIdentifier -eq $serviceIdentity -and ($ace.AccessMask -band 1)) { $found = $true }
        }
        if (-not $found) {
            $ace = New-Object Security.AccessControl.CommonAce([Security.AccessControl.AceFlags]::None,[Security.AccessControl.AceQualifier]::AccessAllowed,1,$serviceIdentity,$false,$null)
            $descriptor.DiscretionaryAcl.InsertAce($descriptor.DiscretionaryAcl.Count,$ace)
            $channel.SecurityDescriptor = $descriptor.GetSddlForm([Security.AccessControl.AccessControlSections]::All)
            $channel.SaveChanges()
        }
    } catch [System.Diagnostics.Eventing.Reader.EventLogNotFoundException] {
        Write-Warning ("未找到日志通道 {0}，请检查相应服务和采集状态。" -f $channelName)
    } finally { if ($channel) { $channel.Dispose() } }
    }
}
Protect-Directory $installDir
function Test-FixedUpdaterTask($Task, [string]$Shell, [string]$Runner) {
    if (-not $Task) { return $true }
    $allowedArguments = @(
        ('-NoProfile -NonInteractive -File "{0}"' -f $Runner)
    )
    return $Task.Actions.Count -eq 1 -and $Task.Actions[0].Execute -eq $Shell -and $allowedArguments -contains $Task.Actions[0].Arguments
}
function Initialize-FixedUpdater {
    Protect-Directory $scriptsDir
    $localInstaller = Join-Path $scriptsDir 'update-agent.ps1'
    $runner = Join-Path $scriptsDir 'poll-update.ps1'
    Assert-NoReparsePoint $localInstaller
    Assert-NoReparsePoint $runner
    $uninstaller = Join-Path $scriptsDir 'uninstall-agent.ps1'
    Assert-NoReparsePoint $uninstaller
    Copy-Item -LiteralPath (Join-Path $staging 'uninstall-agent.ps1') -Destination $uninstaller -Force
    Protect-File $uninstaller
    if ([IO.Path]::GetFullPath($PSCommandPath) -ne $localInstaller) { Copy-Item -LiteralPath $PSCommandPath -Destination $localInstaller -Force }
    @'
#Requires -RunAsAdministrator
$ErrorActionPreference = 'Stop'
$ProgressPreference = 'SilentlyContinue'
Write-Output "`nSSHGuard Agent · 安装与更新"
Write-Output '[1/4] 检查系统与依赖'
$dir = Split-Path -Parent $PSCommandPath
$worker = Join-Path $dir (([Guid]::NewGuid().ToString()) + '.exe')
try {
    Copy-Item -LiteralPath (Join-Path (Split-Path -Parent $dir) 'sshguard-agent.exe') -Destination $worker
    & $worker -poll-update -config (Join-Path $env:ProgramData 'SSHGuard\Agent\agent.json')
    if ($LASTEXITCODE -ne 0) { throw 'Agent 更新失败，请在控制台查看更新状态。' }
} finally { if (Test-Path -LiteralPath $worker) { Remove-Item -LiteralPath $worker -Force } }
'@ | Set-Content -LiteralPath $runner -Encoding UTF8
    Protect-File $localInstaller
    Protect-File $runner
    $shell = Join-Path $env:SystemRoot 'System32\WindowsPowerShell\v1.0\powershell.exe'
    $arguments = '-NoProfile -NonInteractive -File "{0}"' -f $runner
    $existingTask = Get-ScheduledTask -TaskName SSHGuardAgentUpdate -ErrorAction SilentlyContinue
    if (-not (Test-FixedUpdaterTask $existingTask $shell $runner)) { throw '同名更新任务属于其他安装，请先核对任务配置。' }
    $action = New-ScheduledTaskAction -Execute $shell -Argument $arguments
    $trigger = New-ScheduledTaskTrigger -Once -At (Get-Date).AddMinutes(1) -RepetitionInterval (New-TimeSpan -Minutes 1)
    $principal = New-ScheduledTaskPrincipal -UserId 'SYSTEM' -LogonType ServiceAccount -RunLevel Highest
    $settings = New-ScheduledTaskSettingsSet -MultipleInstances IgnoreNew -ExecutionTimeLimit (New-TimeSpan -Minutes 9)
    Register-ScheduledTask -TaskName SSHGuardAgentUpdate -Action $action -Trigger $trigger -Principal $principal -Settings $settings -Force | Out-Null
}
Protect-Directory $configDir
Assert-NoReparsePoint $exe
Assert-NoReparsePoint $config
$lockPath = Join-Path $installDir 'install.lock'
Assert-NoReparsePoint $lockPath
# Exclusive handle prevents concurrent installers from stopping/replacing each other.
$installLock = [IO.File]::Open($lockPath, 'OpenOrCreate', 'ReadWrite', 'None')
$staging = Join-Path $installDir ([Guid]::NewGuid().ToString())
New-Item -ItemType Directory -Path $staging | Out-Null
$download = Join-Path $staging 'sshguard-agent.exe'
$checksum = Join-Path $staging 'agent.sha256'
$service = Get-Service -Name SSHGuardAgent -ErrorAction SilentlyContinue
$wasRunning = $service -and $service.Status -eq 'Running'
$enrolled = $false
$replaced = $false
$backup = Join-Path $staging 'previous.exe'
$replaceBackup = Join-Path $staging 'replaced.exe'
$keepBackup = $false
try {
    if ($Update -and (-not $service -or -not (Test-Path -LiteralPath $config) -or -not (Test-Path -LiteralPath $exe))) { throw '尚未安装 Agent，请从控制台复制安装命令。' }
    Write-Output "[2/4] 下载并校验 Agent $targetVersion（$arch）"
    Get-ReleaseAsset 'uninstall-agent.ps1' (Join-Path $staging 'uninstall-agent.ps1') 1048576 60
    Get-ReleaseAsset "sshguard-agent-windows-$arch.exe" $download 134217728 180
    Get-ReleaseAsset "sshguard-agent-windows-$arch.exe.sha256" $checksum 4096 30
    $expected = ((Get-Content -LiteralPath $checksum -Raw).Trim() -split '\s+')[0]
    if ($expected -notmatch '^[a-f0-9]{64}$' -or (Get-FileHash -LiteralPath $download -Algorithm SHA256).Hash -ne $expected) { throw '文件校验失败，已停止安装，请重新下载。' }
    $downloadVersion = & $download -version
    if ($LASTEXITCODE -ne 0) { throw '程序无法运行，请检查系统和架构。' }
    if ($downloadVersion -ne "SSHGuard $targetVersion windows/$arch") { throw '下载版本与目标版本不一致，已停止更新。' }
    Write-Output '[3/4] 配置 Agent 服务'
    if ($Update) {
        & $download -check -endpoint $Endpoint -config $config
        if ($LASTEXITCODE -ne 0) { throw '主控连接或凭证校验失败，原服务未变更。' }
        Copy-Item -LiteralPath $exe -Destination $backup
    }
    if ($service) {
        # Reject a colliding service name before touching the existing service.
        $installed = Get-CimInstance Win32_Service -Filter "Name='SSHGuardAgent'"
        $quotedExe = if ($exe.Contains(' ')) { '"{0}"' -f $exe } else { $exe }
        $quotedConfig = if ($config.Contains(' ')) { '"{0}"' -f $config } else { $config }
        $expectedCommand = '{0} -config {1}' -f $quotedExe, $quotedConfig
        if ($installed.PathName -ne $expectedCommand) { throw '同名服务属于其他安装，请先核对服务路径。' }
        if ($Update -and (Get-FileHash -LiteralPath $exe -Algorithm SHA256).Hash -eq $expected) {
            Protect-File $config
            if (-not $wasRunning) { Start-Service -Name SSHGuardAgent; (Get-Service SSHGuardAgent).WaitForStatus('Running',[TimeSpan]::FromSeconds(30)) }
            Initialize-FixedUpdater
            Write-Output '[完成] 已是最新版本，服务运行正常。'
            return
        }
        Stop-Service -Name SSHGuardAgent
        (Get-Service SSHGuardAgent).WaitForStatus('Stopped',[TimeSpan]::FromSeconds(30))
    }
    # Token travels over stdin; only the short-lived install command contains it.
    if (-not $Update) {
        # PowerShell 5.1 hosts can send a UTF-8 BOM (or UTF-16) to native stdin.
        # The token alphabet is ASCII; use a deterministic BOM-free encoding.
        $previousEncoding = $OutputEncoding
        try {
            $OutputEncoding = New-Object Text.ASCIIEncoding
            $httpOption = '-allow-insecure-http=' + $AllowInsecureHttp.IsPresent.ToString().ToLowerInvariant()
            $EnrollmentToken | & $download -enroll -endpoint $Endpoint -config $config $httpOption
        } finally { $OutputEncoding = $previousEncoding }
        if ($LASTEXITCODE -ne 0) { throw '注册失败，请在控制台重新生成安装命令。' }
        $enrolled = $true
        # Assigning null to a ValidatePattern parameter raises in PowerShell 5.1.
        [void]$PSBoundParameters.Remove('EnrollmentToken')
        Remove-Variable -Name EnrollmentToken
    }
    # Replace the file ACL too; do not retain explicit permissions from an older config.
    Protect-File $config
    if ($Update) { $replaced = $true }
    # PS 5.1 coerces $null string arguments to an empty path; use a real backup.
    if (Test-Path -LiteralPath $exe) { [IO.File]::Replace($download,$exe,$replaceBackup) }
    else { Move-Item -LiteralPath $download -Destination $exe }
    Protect-File $exe
    & $exe -service install -config $config
    if ($LASTEXITCODE -ne 0) { throw 'Windows service registration failed; see the preceding error.' }
    Initialize-CollectorAccess
    Start-Service -Name SSHGuardAgent
    (Get-Service SSHGuardAgent).WaitForStatus('Running',[TimeSpan]::FromSeconds(30))
    Start-Sleep -Seconds 2
    if ((Get-Service SSHGuardAgent).Status -ne 'Running') { throw 'Agent 意外停止，请查看事件查看器中的 SSHGuardAgent 日志。' }
    & $exe -check -endpoint $Endpoint -config $config
    if ($LASTEXITCODE -ne 0) { throw '连接主控失败，请检查地址、网络及 Agent 日志。' }
    Initialize-FixedUpdater
    $replaced = $false
    Write-Output '[4/4] 服务检查通过'
    Write-Output "`n[完成] Agent 已启动，请在控制台确认在线。"
    Write-Output '状态：Get-Service SSHGuardAgent'
    Write-Output '日志：事件查看器 → 应用程序 → SSHGuardAgent'
} catch {
    if ($replaced -and (Test-Path -LiteralPath $backup)) {
        $keepBackup = $true
        Stop-Service -Name SSHGuardAgent -ErrorAction Continue
        (Get-Service SSHGuardAgent).WaitForStatus('Stopped',[TimeSpan]::FromSeconds(30))
        Copy-Item -LiteralPath $backup -Destination $exe -Force
        Protect-File $exe
        $keepBackup = $false
        Write-Warning '更新失败，已恢复旧版 Agent。'
    }
    if ($wasRunning -and -not $enrolled) { Start-Service -Name SSHGuardAgent -ErrorAction Continue }
    throw
} finally {
    try {
    # Only delete the exact temporary files created by this invocation.
    $files = @($download,$checksum,$replaceBackup,(Join-Path $staging 'uninstall-agent.ps1'))
    if (-not $keepBackup) { $files += $backup }
    foreach ($file in $files) { if (Test-Path -LiteralPath $file) { Remove-Item -LiteralPath $file -Force } }
    if (-not $keepBackup -and (Test-Path -LiteralPath $staging)) { Remove-Item -LiteralPath $staging }
    if ($keepBackup) { Write-Warning "恢复失败，备份已保留：$backup" }
    } finally { $installLock.Dispose() }
}
