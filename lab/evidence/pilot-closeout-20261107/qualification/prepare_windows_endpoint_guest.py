"""Prepare a disposable Windows Server 2025 Core eval VM; keep secret/media in EVE."""
from pathlib import Path
import secrets
import subprocess
from xml.sax.saxutils import escape

root=Path('/root/nac-qualification/windows-2025-endpoint')
iso=Path('/root/nac-qualification/windows-server-eval-nac.iso')
if root.exists() or not iso.is_file():
    raise SystemExit('target exists or authorized evaluation ISO missing')
root.mkdir(mode=0o700)
password=secrets.token_urlsafe(24)
(root/'admin.secret').write_text(password+'\n')
(root/'admin.secret').chmod(0o600)
command=('powershell.exe -NoProfile -ExecutionPolicy Bypass -Command '
         '"Set-Service sshd -StartupType Automatic; Start-Service sshd; '
         'New-NetFirewallRule -DisplayName NACLabSSHD -Direction Inbound '
         '-Action Allow -Protocol TCP -LocalPort 22"')
xml=f'''<?xml version="1.0" encoding="utf-8"?>
<unattend xmlns="urn:schemas-microsoft-com:unattend" xmlns:wcm="http://schemas.microsoft.com/WMIConfig/2002/State">
  <settings pass="windowsPE">
    <component name="Microsoft-Windows-International-Core-WinPE" processorArchitecture="amd64" publicKeyToken="31bf3856ad364e35" language="neutral" versionScope="nonSxS">
      <SetupUILanguage><UILanguage>en-US</UILanguage></SetupUILanguage>
      <InputLocale>en-US</InputLocale><SystemLocale>en-US</SystemLocale><UILanguage>en-US</UILanguage><UserLocale>en-US</UserLocale>
    </component>
    <component name="Microsoft-Windows-Setup" processorArchitecture="amd64" publicKeyToken="31bf3856ad364e35" language="neutral" versionScope="nonSxS">
      <DiskConfiguration><Disk wcm:action="add"><DiskID>0</DiskID><WillWipeDisk>true</WillWipeDisk>
        <CreatePartitions>
          <CreatePartition wcm:action="add"><Order>1</Order><Type>Primary</Type><Size>500</Size></CreatePartition>
          <CreatePartition wcm:action="add"><Order>2</Order><Type>Primary</Type><Extend>true</Extend></CreatePartition>
        </CreatePartitions>
        <ModifyPartitions>
          <ModifyPartition wcm:action="add"><Order>1</Order><PartitionID>1</PartitionID><Format>NTFS</Format><Label>System</Label><Active>true</Active></ModifyPartition>
          <ModifyPartition wcm:action="add"><Order>2</Order><PartitionID>2</PartitionID><Format>NTFS</Format><Label>Windows</Label></ModifyPartition>
        </ModifyPartitions>
      </Disk><WillShowUI>OnError</WillShowUI></DiskConfiguration>
      <ImageInstall><OSImage><InstallFrom><MetaData wcm:action="add"><Key>/IMAGE/INDEX</Key><Value>1</Value></MetaData></InstallFrom>
        <InstallTo><DiskID>0</DiskID><PartitionID>2</PartitionID></InstallTo><WillShowUI>OnError</WillShowUI>
      </OSImage></ImageInstall>
      <UserData><AcceptEula>true</AcceptEula><FullName>NAC Lab</FullName><Organization>NAC Lab</Organization></UserData>
    </component>
  </settings>
  <settings pass="specialize">
    <component name="Microsoft-Windows-Deployment" processorArchitecture="amd64" publicKeyToken="31bf3856ad364e35" language="neutral" versionScope="nonSxS">
      <RunSynchronous><RunSynchronousCommand wcm:action="add"><Order>1</Order><Description>Enable temporary lab setup SSH</Description><Path>{escape(command)}</Path></RunSynchronousCommand></RunSynchronous>
    </component>
  </settings>
  <settings pass="oobeSystem">
    <component name="Microsoft-Windows-Shell-Setup" processorArchitecture="amd64" publicKeyToken="31bf3856ad364e35" language="neutral" versionScope="nonSxS">
      <UserAccounts><AdministratorPassword><Value>{escape(password)}</Value><PlainText>true</PlainText></AdministratorPassword></UserAccounts>
      <OOBE><HideEULAPage>true</HideEULAPage><HideOnlineAccountScreens>true</HideOnlineAccountScreens><HideWirelessSetupInOOBE>true</HideWirelessSetupInOOBE><NetworkLocation>Work</NetworkLocation><ProtectYourPC>3</ProtectYourPC></OOBE>
      <TimeZone>UTC</TimeZone>
    </component>
  </settings>
</unattend>
'''
(root/'Autounattend.xml').write_text(xml)
(root/'Autounattend.xml').chmod(0o600)
floppy=root/'unattend.img'
subprocess.run(['truncate','-s','2880K',str(floppy)],check=True)
subprocess.run(['mkfs.vfat','-F','12',str(floppy)],check=True,capture_output=True)
subprocess.run(['mcopy','-i',str(floppy),str(root/'Autounattend.xml'),'::Autounattend.xml'],check=True,capture_output=True)
floppy.chmod(0o600)
disk=root/'windows.qcow2'
subprocess.run(['qemu-img','create','-f','qcow2',str(disk),'32G'],check=True,capture_output=True)
print('WINDOWS_EVAL_PREPARED image_index=1 name=SERVERSTANDARDCORE disk_virtual=32GiB boot_media=official_iso+private_unattend_floppy')
print('LAB_ADMIN_SECRET_STORED_ONLY_IN_EVE')
