$ErrorActionPreference = 'Stop'
$ProgressPreference = 'SilentlyContinue'
$root = 'C:\NACLab'
if (-not (Get-Service dot3svc -ErrorAction SilentlyContinue)) {
    throw 'Wired AutoConfig (dot3svc) is absent; this Windows image cannot be a native wired EAP-TLS endpoint.'
}
if (-not (Test-Path "$root\wired-client.pfx")) { throw 'Disposable client PFX missing' }
$ca = Import-Certificate -FilePath "$root\ca.crt" -CertStoreLocation 'Cert:\LocalMachine\Root'
$plain = (Get-Content -Raw "$root\pfx.secret").Trim()
$secure = ConvertTo-SecureString -String $plain -AsPlainText -Force
$client = Import-PfxCertificate -FilePath "$root\wired-client.pfx" -CertStoreLocation 'Cert:\LocalMachine\My' -Password $secure
$plain = $null
$secure = $null
Remove-Item "$root\pfx.secret","$root\wired-client.pfx" -Force
Set-Service dot3svc -StartupType Automatic
Start-Service dot3svc
$xml = @'
<?xml version="1.0" encoding="US-ASCII"?>
<LANProfile xmlns="https://www.microsoft.com/networking/LAN/profile/v1">
  <MSM><security>
    <OneXEnforced>true</OneXEnforced>
    <OneXEnabled>true</OneXEnabled>
    <OneX xmlns="https://www.microsoft.com/networking/OneX/v1">
      <authMode>machine</authMode>
      <EAPConfig>
        <EapHostConfig xmlns="https://www.microsoft.com/provisioning/EapHostConfig"
            xmlns:eapCommon="https://www.microsoft.com/provisioning/EapCommon"
            xmlns:baseEap="https://www.microsoft.com/provisioning/BaseEapMethodConfig">
          <EapMethod><eapCommon:Type>13</eapCommon:Type><eapCommon:AuthorId>0</eapCommon:AuthorId></EapMethod>
          <Config xmlns:baseEap="https://www.microsoft.com/provisioning/BaseEapConnectionPropertiesV1"
              xmlns:eapTls="https://www.microsoft.com/provisioning/EapTlsConnectionPropertiesV1">
            <baseEap:Eap>
              <baseEap:Type>13</baseEap:Type>
              <eapTls:EapType>
                <eapTls:CredentialsSource><eapTls:CertificateStore /></eapTls:CredentialsSource>
                <eapTls:ServerValidation>
                  <eapTls:DisableUserPromptForServerValidation>true</eapTls:DisableUserPromptForServerValidation>
                  <eapTls:ServerNames>radius.nac.lab</eapTls:ServerNames>
                </eapTls:ServerValidation>
                <eapTls:DifferentUsername>false</eapTls:DifferentUsername>
              </eapTls:EapType>
            </baseEap:Eap>
          </Config>
        </EapHostConfig>
      </EAPConfig>
    </OneX>
  </security></MSM>
</LANProfile>
'@
$xml = $xml.Replace('https://www.microsoft.com/','http://www.microsoft.com/')
[System.IO.File]::WriteAllText("$root\wired.xml", $xml, [System.Text.Encoding]::ASCII)
$output = & netsh.exe lan add profile "filename=$root\wired.xml" 2>&1
if ($LASTEXITCODE -ne 0) { throw "netsh lan add profile failed: $output" }
Write-Output "WINDOWS_WIRED_PROFILE $output"
Write-Output "DOT3SVC $((Get-Service dot3svc).Status)"
Write-Output "CLIENT_CERT_SUBJECT $($client.Subject)"
Write-Output "CA_THUMBPRINT $($ca.Thumbprint)"
Write-Output "PROFILE_SOURCE Microsoft machine certificate EAP-TLS wired profile sample"
