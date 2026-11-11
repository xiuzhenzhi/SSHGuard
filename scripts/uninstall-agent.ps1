#Requires -Version 5.1
#Requires -RunAsAdministrator
$ErrorActionPreference = 'Stop'
function Test-FixedUpdaterTask($Task, [string]$Shell, [string]$Runner) {
    if (-not $Task) { return $true }
    $allowedArguments = @(
        ('-NoProfile -NonInteractive -File "{0}"' -f $Runner)
        ('-NoProfile -NonInteractive -ExecutionPolicy RemoteSigned -File "{0}"' -f $Runner)
    )
    return $Task.Actions.Count -eq 1 -and $Task.Actions[0].Execute -eq $Shell -and $allowedArguments -contains $Task.Actions[0].Arguments
}
# Resolve and validate fixed targets and ancestors before any recursive removal.
function Assert-CleanupPath([string]$Path, [string]$Root) {
    $full = [IO.Path]::GetFullPath($Path)
    $base = [IO.Path]::GetFullPath($Root).TrimEnd('\')
    if (-not $full.StartsWith($base + '\', [StringComparison]::OrdinalIgnoreCase)) { throw 'Cleanup path outside expected root.' }
    $current = $full
    while ($current) {
        $item = Get-Item -LiteralPath $current -Force -ErrorAction SilentlyContinue
        if ($item -and ($item.Attributes -band [IO.FileAttributes]::ReparsePoint)) { throw "Refusing reparse point: $current" }
        $current = Split-Path -Parent $current
    }
    # Reject nested junctions as well; never traverse them during cleanup.
    if (Test-Path -LiteralPath $full -PathType Container) {
        foreach ($child in Get-ChildItem -LiteralPath $full -Force) {
            if ($child.Attributes -band [IO.FileAttributes]::ReparsePoint) { throw "Refusing reparse point: $($child.FullName)" }
            if ($child.PSIsContainer) { Assert-CleanupPath $child.FullName $Root }
        }
    }
}
$exe = Join-Path $env:ProgramData 'SSHGuard\Agent\sshguard-agent.exe'
$config = Join-Path $env:ProgramData 'SSHGuard\Agent\agent.json'
$state = Join-Path $env:ProgramData 'SSHGuard\Agent\sshguard-agent'
Assert-CleanupPath (Split-Path -Parent $exe) $env:ProgramData
Assert-CleanupPath $config $env:ProgramData
Assert-CleanupPath $state $env:ProgramData
$installLock = $null
$completed = $false
$installDir = Split-Path -Parent $exe
foreach ($path in @($installDir,$exe)) {
    if ((Get-Item -LiteralPath $path -Force -ErrorAction SilentlyContinue).Attributes -band [IO.FileAttributes]::ReparsePoint) { throw "Refusing reparse point: $path" }
}
if (Test-Path -LiteralPath $installDir) {
    $lockPath = Join-Path $installDir 'install.lock'
    if ((Get-Item -LiteralPath $lockPath -Force -ErrorAction SilentlyContinue).Attributes -band [IO.FileAttributes]::ReparsePoint) { throw 'Refusing reparse point lock.' }
    $installLock = [IO.File]::Open($lockPath, 'OpenOrCreate', 'ReadWrite', 'None')
}
try {
$runner = Join-Path $env:ProgramData 'SSHGuard\Agent\sshguard\poll-update.ps1'
$task = Get-ScheduledTask -TaskName SSHGuardAgentUpdate -ErrorAction SilentlyContinue
if ($task) {
    $shell = Join-Path $env:SystemRoot 'System32\WindowsPowerShell\v1.0\powershell.exe'
    if (-not (Test-FixedUpdaterTask $task $shell $runner)) { throw 'Updater task belongs to a different installation.' }
    Disable-ScheduledTask -TaskName SSHGuardAgentUpdate | Out-Null
    if ($task.State -eq 'Running') { throw 'Updater is running; wait for it to finish and rerun uninstall.' }
    Unregister-ScheduledTask -TaskName SSHGuardAgentUpdate -Confirm:$false
}
if (Get-Service SSHGuardAgent -ErrorAction SilentlyContinue) {
    $installed = Get-CimInstance Win32_Service -Filter "Name='SSHGuardAgent'"
    $quotedExe = if ($exe.Contains(' ')) { '"{0}"' -f $exe } else { $exe }
    $quotedConfig = if ($config.Contains(' ')) { '"{0}"' -f $config } else { $config }
    if ($installed.PathName -ne ('{0} -config {1}' -f $quotedExe, $quotedConfig)) { throw 'Existing service belongs to a different installation.' }
    Stop-Service SSHGuardAgent
    (Get-Service SSHGuardAgent).WaitForStatus('Stopped',[TimeSpan]::FromSeconds(30))
    & $exe -service uninstall -config $config
    if ($LASTEXITCODE -ne 0) { throw 'Service removal failed; executable and config retained.' }
}
if (Test-Path -LiteralPath $exe) { Remove-Item -LiteralPath $exe }
foreach ($path in @($runner,(Join-Path $env:ProgramData 'SSHGuard\Agent\sshguard\update-agent.ps1'),(Join-Path $env:ProgramData 'SSHGuard\Agent\sshguard\uninstall-agent.ps1'))) {
    if (Test-Path -LiteralPath $path) { Remove-Item -LiteralPath $path }
}
if ([Diagnostics.EventLog]::SourceExists('SSHGuardAgent')) { Remove-EventLog -Source SSHGuardAgent }
foreach ($path in @($config,$state)) {
    Assert-CleanupPath $path $env:ProgramData
    if (Test-Path -LiteralPath $path) { Remove-Item -LiteralPath $path -Recurse -Force }
}
$completed = $true

} finally { if ($installLock) { $installLock.Dispose() } }

if ($completed) {
    Assert-CleanupPath $installDir $env:ProgramData
    if (Test-Path -LiteralPath $installDir) { Remove-Item -LiteralPath $installDir -Recurse -Force }
    $configDir = Split-Path -Parent $config
    if ((Test-Path -LiteralPath $configDir) -and -not (Get-ChildItem -LiteralPath $configDir -Force | Select-Object -First 1)) { Remove-Item -LiteralPath $configDir }
    Write-Output 'Agent removed. Local configuration, credentials, queue, cursors, quarantine and event source deleted. Revoke credentials or delete the server in the dashboard. Shared Windows event logs are not cleared.'
}
