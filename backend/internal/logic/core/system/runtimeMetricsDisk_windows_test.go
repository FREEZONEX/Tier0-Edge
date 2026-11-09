// TASK-028 runtimeMetricsDisk_windows 单元测试：
//
//	目标：覆盖 Windows 平台 diskUsage 的 GetDiskFreeSpaceEx 解析路径。
//	说明：本文件仅在 GOOS=windows 下编译（//go:build windows），
//	      当前 CI/本地为 Linux，测试不参与编译；此文件仅为满足文件级覆盖台账。
package system
