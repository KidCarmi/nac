"""Convert QEMU screendump PPM to PNG using only the Python standard library."""
from pathlib import Path
import struct
import sys
import zlib

source, destination = map(Path, sys.argv[1:3])
with source.open('rb') as f:
    assert f.readline().strip() == b'P6'
    dims=f.readline()
    while dims.startswith(b'#'): dims=f.readline()
    width,height=map(int,dims.split())
    assert f.readline().strip() == b'255'
    rgb=f.read()
assert len(rgb)==width*height*3
scan=b''.join(b'\0'+rgb[y*width*3:(y+1)*width*3] for y in range(height))
def chunk(kind,data):
    return struct.pack('>I',len(data))+kind+data+struct.pack('>I',zlib.crc32(kind+data)&0xffffffff)
png=b'\x89PNG\r\n\x1a\n'+chunk(b'IHDR',struct.pack('>IIBBBBB',width,height,8,2,0,0,0))+chunk(b'IDAT',zlib.compress(scan))+chunk(b'IEND',b'')
destination.write_bytes(png)
print(width,height,destination)
