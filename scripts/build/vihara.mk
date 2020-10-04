# Copyright 2020 The arhat.dev Authors.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

# native
vihara:
	sh scripts/build/build.sh $@

# linux
vihara.linux.amd64:
	sh scripts/build/build.sh $@

vihara.linux.arm64:
	sh scripts/build/build.sh $@

vihara.linux.armv7:
	sh scripts/build/build.sh $@

vihara.linux.armv6:
	sh scripts/build/build.sh $@

vihara.linux.armv5:
	sh scripts/build/build.sh $@

vihara.linux.x86:
	sh scripts/build/build.sh $@

vihara.linux.ppc64le:
	sh scripts/build/build.sh $@

vihara.linux.mips64le:
	sh scripts/build/build.sh $@

vihara.linux.s390x:
	sh scripts/build/build.sh $@

vihara.linux.all: \
	vihara.linux.amd64 \
	vihara.linux.arm64 \
	vihara.linux.armv7 \
	vihara.linux.armv6 \
	vihara.linux.armv5 \
	vihara.linux.x86 \
	vihara.linux.ppc64le \
	vihara.linux.mips64le \
	vihara.linux.s390x

# windows
vihara.windows.amd64:
	sh scripts/build/build.sh $@

vihara.windows.armv7:
	sh scripts/build/build.sh $@

vihara.windows.all: \
	vihara.windows.amd64 \
	vihara.windows.armv7
