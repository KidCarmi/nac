$ErrorActionPreference = 'Continue'
$ProgressPreference = 'SilentlyContinue'
$root = 'C:\NACLab'
$path = Join-Path $root 'probe.jsonl'
$deadline = [DateTime]::UtcNow.AddMinutes(75)
$limit = 8MB
$writer = [System.IO.StreamWriter]::new($path, $true, [System.Text.UTF8Encoding]::new($false))
$writer.AutoFlush = $true

function Write-Record($record) {
    $record.at_utc = [DateTime]::UtcNow.ToString('yyyy-MM-ddTHH:mm:ss.fffZ')
    $writer.WriteLine(($record | ConvertTo-Json -Depth 7 -Compress))
    if ((Get-Item $path).Length -ge $limit) { throw 'Probe reached 8 MiB output bound' }
}
function Probe-Tcp([string]$segment, [string]$address) {
    $client = [System.Net.Sockets.TcpClient]::new()
    $start = [System.Diagnostics.Stopwatch]::StartNew()
    $connected = $false
    $source = $null
    try {
        $async = $client.BeginConnect($address, 8080, $null, $null)
        if ($async.AsyncWaitHandle.WaitOne(600)) {
            $client.EndConnect($async)
            $connected = $true
            $source = [string]$client.Client.LocalEndPoint
        }
        $async.AsyncWaitHandle.Close()
    } catch { }
    finally { $client.Close(); $start.Stop() }
    Write-Record @{ event='tcp_probe'; segment=$segment; connected=$connected;
                    source=$source; elapsed_ms=$start.ElapsedMilliseconds }
}
function Network-State {
    $up = @(Get-NetAdapter -ErrorAction SilentlyContinue | Where-Object Status -eq Up)
    $adapters = @($up | ForEach-Object { @{ name=$_.Name; mac=$_.MacAddress; index=$_.ifIndex } })
    $addresses = @()
    $routes = @()
    $dns = @()
    $lease = @()
    foreach ($nic in $up) {
        $addresses += @(Get-NetIPAddress -InterfaceIndex $nic.ifIndex -AddressFamily IPv4 -ErrorAction SilentlyContinue |
                        ForEach-Object { $_.IPAddress + '/' + $_.PrefixLength })
        $routes += @(Get-NetRoute -InterfaceIndex $nic.ifIndex -AddressFamily IPv4 -ErrorAction SilentlyContinue |
                     Where-Object DestinationPrefix -eq '0.0.0.0/0' |
                     ForEach-Object { $_.NextHop })
        $dns += @(Get-DnsClientServerAddress -InterfaceIndex $nic.ifIndex -AddressFamily IPv4 -ErrorAction SilentlyContinue |
                  ForEach-Object { $_.ServerAddresses })
        $lease += @(Get-CimInstance Win32_NetworkAdapterConfiguration -Filter "InterfaceIndex=$($nic.ifIndex)" -ErrorAction SilentlyContinue |
                   ForEach-Object { @{ obtained=$_.DHCPLeaseObtained; expires=$_.DHCPLeaseExpires;
                                      dhcp_server=$_.DHCPServer } })
    }
    return @{ adapters=$adapters; addresses=$addresses; routes=$routes; dns=$dns; lease=$lease }
}

try {
    Write-Record @{ event='start'; os=[Environment]::OSVersion.VersionString;
                    interfaces=@(Get-NetAdapter | ForEach-Object { $_.Name + ':' + $_.MacAddress }) }
    $last = ''
    $lastNetwork = [DateTime]::MinValue
    while ([DateTime]::UtcNow -lt $deadline) {
        if (([DateTime]::UtcNow - $lastNetwork).TotalSeconds -ge 3) {
            $network = Network-State
            $rendered = $network | ConvertTo-Json -Depth 7 -Compress
            if ($rendered -ne $last) { Write-Record (@{ event='network_state' } + $network); $last=$rendered }
            $lastNetwork = [DateTime]::UtcNow
        }
        Probe-Tcp 'corp' '10.70.10.10'
        Probe-Tcp 'quarantine' '10.70.20.10'
        Start-Sleep -Milliseconds 250
    }
    Write-Record @{ event='end' }
} catch {
    Write-Record @{ event='error'; detail=$_.Exception.Message }
} finally { $writer.Close() }
