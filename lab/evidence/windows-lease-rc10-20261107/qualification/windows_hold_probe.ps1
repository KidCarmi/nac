$ErrorActionPreference = 'Stop'
$ProgressPreference = 'SilentlyContinue'
$path = 'C:\NACLab\hold-probe.jsonl'
$deadline = [DateTime]::UtcNow.AddMinutes(40)
$limit = 8MB
$writer = [System.IO.StreamWriter]::new($path, $false, [System.Text.UTF8Encoding]::new($false))
$writer.AutoFlush = $true
function Record([string]$eventName, [int]$sequence, [string]$detail) {
    $item = @{ at_utc = [DateTime]::UtcNow.ToString('yyyy-MM-ddTHH:mm:ss.fffZ');
               event = $eventName; sequence = $sequence; detail = $detail }
    $writer.WriteLine(($item | ConvertTo-Json -Compress))
    if ((Get-Item $path).Length -ge $limit) { throw 'hold probe reached 8 MiB bound' }
}
$client = [System.Net.Sockets.TcpClient]::new()
try {
    $client.Connect('10.70.10.10', 8081)
    $stream = $client.GetStream()
    $stream.ReadTimeout = 1500
    $stream.WriteTimeout = 1500
    Record 'connected' 0 ([string]$client.Client.LocalEndPoint)
    $sequence = 0
    while ([DateTime]::UtcNow -lt $deadline) {
        $sequence++
        $message = [Text.Encoding]::ASCII.GetBytes(('ping-{0:D8}' -f $sequence))
        try {
            $stream.Write($message, 0, $message.Length)
            $reply = [byte[]]::new($message.Length)
            $count = $stream.Read($reply, 0, $reply.Length)
            if ($count -eq 0) { Record 'closed' $sequence 'remote closed'; break }
            Record 'reply' $sequence ([Text.Encoding]::ASCII.GetString($reply, 0, $count))
        } catch {
            Record 'error' $sequence $_.Exception.GetType().Name
            break
        }
        Start-Sleep -Milliseconds 500
    }
    Record 'end' $sequence 'single connection; no reconnect'
} catch {
    Record 'connect_error' 0 $_.Exception.GetType().Name
} finally {
    $client.Close()
    $writer.Close()
}
