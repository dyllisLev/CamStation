# Paste all of this into Administrator Windows PowerShell on 10.0.0.30.
$ErrorActionPreference = 'Stop'
$target = '10.0.0.30'
$source = '10.0.0.16'
$user = 'CamStationBuildOps'
$pub = 'ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIDwkCkfP4iqYy0OvWCuIUjP1Mq+fQvBLWg5ZOoAX5uo7 camstation-buildops-10.0.0.30-2026-08-10'

$id = [Security.Principal.WindowsIdentity]::GetCurrent()
$isAdmin = [Security.Principal.WindowsPrincipal]::new($id).IsInRole(
    [Security.Principal.WindowsBuiltInRole]::Administrator
)
if (-not $isAdmin) { throw 'Run Windows PowerShell as Administrator.' }
if (@(Get-NetIPAddress -AddressFamily IPv4 | Where-Object IPAddress -eq $target).Count -ne 1) {
    throw "This PC is not $target."
}

$cap = 'OpenSSH.Server~~~~0.0.1.0'
if ((Get-WindowsCapability -Online -Name $cap).State -ne 'Installed') {
    Add-WindowsCapability -Online -Name $cap | Out-Null
}
if ($null -ne (Get-LocalUser -Name $user -ErrorAction SilentlyContinue)) {
    throw "$user already exists; stop and return this message."
}

$sshDir = Join-Path $env:ProgramData 'ssh'
$keyFile = Join-Path $sshDir 'administrators_authorized_keys'
New-Item -ItemType Directory -Path $sshDir -Force | Out-Null
if ((Test-Path $keyFile) -and @(Get-Content $keyFile | Where-Object { $_.Trim() }).Count -gt 0) {
    throw 'An administrator SSH key already exists; stop and return this message.'
}

$password = ConvertTo-SecureString ('Aa1!' + [Guid]::NewGuid().ToString('N')) -AsPlainText -Force
$account = New-LocalUser -Name $user -Password $password -Description 'CamStation SSH bootstrap' `
    -AccountNeverExpires -PasswordNeverExpires -UserMayNotChangePassword
$admins = Get-LocalGroup -SID 'S-1-5-32-544'
Add-LocalGroupMember -Group $admins -Member $account

[IO.File]::WriteAllText($keyFile, "$pub`r`n", (New-Object Text.UTF8Encoding($false)))
& icacls.exe $keyFile /inheritance:r /grant:r '*S-1-5-18:(F)' '*S-1-5-32-544:(F)' | Out-Null
if ($LASTEXITCODE -ne 0) { throw 'SSH key ACL failed.' }

Get-NetFirewallRule -Name 'OpenSSH-Server-In-TCP' -ErrorAction SilentlyContinue |
    Disable-NetFirewallRule
Get-NetFirewallRule -Name 'CamStation-BuildOps-SSH-In' -ErrorAction SilentlyContinue |
    Remove-NetFirewallRule
New-NetFirewallRule -Name 'CamStation-BuildOps-SSH-In' -DisplayName 'CamStation BuildOps SSH' `
    -Direction Inbound -Action Allow -Protocol TCP -LocalPort 22 `
    -LocalAddress $target -RemoteAddress $source -Profile Any | Out-Null

Set-Service sshd -StartupType Automatic
Start-Service sshd
$hostKey = Join-Path $sshDir 'ssh_host_ed25519_key.pub'
$hostFP = @(& "$env:WINDIR\System32\OpenSSH\ssh-keygen.exe" -lf $hostKey -E sha256)
[pscustomobject]@{
    Result = 'SSH_BOOTSTRAP_READY'
    ComputerName = $env:COMPUTERNAME
    LoginUser = $user
    AllowedSource = $source
    HostKeyFingerprint = ($hostFP -join ' ')
    SshdStatus = (Get-Service sshd).Status
} | Format-List
