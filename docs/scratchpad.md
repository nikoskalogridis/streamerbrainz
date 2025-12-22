
## Monitoring user space service logs
```bash
journalctl _UID=$(id -u) _SYSTEMD_USER_UNIT=<name_of_service>.service -f
```
