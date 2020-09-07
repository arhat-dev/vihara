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

# build
image.build.vihara.linux.x86:
	sh scripts/image/build.sh $@

image.build.vihara.linux.amd64:
	sh scripts/image/build.sh $@

image.build.vihara.linux.armv6:
	sh scripts/image/build.sh $@

image.build.vihara.linux.armv7:
	sh scripts/image/build.sh $@

image.build.vihara.linux.arm64:
	sh scripts/image/build.sh $@

image.build.vihara.linux.ppc64le:
	sh scripts/image/build.sh $@

image.build.vihara.linux.s390x:
	sh scripts/image/build.sh $@

image.build.vihara.linux.all: \
	image.build.vihara.linux.amd64 \
	image.build.vihara.linux.arm64 \
	image.build.vihara.linux.armv7 \
	image.build.vihara.linux.armv6 \
	image.build.vihara.linux.x86 \
	image.build.vihara.linux.s390x \
	image.build.vihara.linux.ppc64le

image.build.vihara.windows.amd64:
	sh scripts/image/build.sh $@

image.build.vihara.windows.armv7:
	sh scripts/image/build.sh $@

image.build.vihara.windows.all: \
	image.build.vihara.windows.amd64 \
	image.build.vihara.windows.armv7

# push
image.push.vihara.linux.x86:
	sh scripts/image/push.sh $@

image.push.vihara.linux.amd64:
	sh scripts/image/push.sh $@

image.push.vihara.linux.armv6:
	sh scripts/image/push.sh $@

image.push.vihara.linux.armv7:
	sh scripts/image/push.sh $@

image.push.vihara.linux.arm64:
	sh scripts/image/push.sh $@

image.push.vihara.linux.ppc64le:
	sh scripts/image/push.sh $@

image.push.vihara.linux.s390x:
	sh scripts/image/push.sh $@

image.push.vihara.linux.all: \
	image.push.vihara.linux.amd64 \
	image.push.vihara.linux.arm64 \
	image.push.vihara.linux.armv7 \
	image.push.vihara.linux.armv6 \
	image.push.vihara.linux.x86 \
	image.push.vihara.linux.s390x \
	image.push.vihara.linux.ppc64le

image.push.vihara.windows.amd64:
	sh scripts/image/push.sh $@

image.push.vihara.windows.armv7:
	sh scripts/image/push.sh $@

image.push.vihara.windows.all: \
	image.push.vihara.windows.amd64 \
	image.push.vihara.windows.armv7
