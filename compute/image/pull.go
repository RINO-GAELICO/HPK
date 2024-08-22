// Copyright © 2023 FORTH-ICS
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package image

import (
	"strings"
	"fmt"
	"github.com/carv-ics-forth/hpk/compute"
	"github.com/carv-ics-forth/hpk/pkg/process"
	"github.com/pkg/errors"
)

func Pull(imageDir string, transport Transport, imageName string) (*Image, error) {
	// Remove the digest from the image, because Singularity fails with
	// "Docker references with both a tag and digest are currently not supported".
	imageName = strings.Split(imageName, "@")[0]

	/*
	
	Keep in mind the ImagePullpolicy implementation for the future 

	*/

	res, err := process.Execute(compute.Environment.PodmanBin, "images", fmt.Sprintf("--format=\"{{.Names}}|{{.IsReadOnly}}\""))
	if err != nil {
		return nil, errors.Wrapf(err, "Failed to check the image")
	}


	cleanOutput := strings.Trim(string(res), "{}\" \n")

	// TODO: 
	// Add the tag if there is no tag appending the latest tag

	// Split the output into lines
	lines := strings.Split(cleanOutput, "\n")

	// Check every line to see if the image already exists
	for _, line := range lines {
		
		// Trim extra quotes and spaces
		line = strings.Trim(line, "\" ")

		// Split by the '|' character
		parts := strings.Split(line, "|")
		if len(parts) != 2 {
			continue
		}

		// Extract image name and condition
		imagePart := strings.Trim(parts[0], "[] ")
		// remove the tag at the end of the image name
		imagePart = strings.Split(imagePart, ":")[0]
		condition := strings.Trim(parts[1], " ")

		// Check if the image name matches and condition is true
		if imagePart == imageName && condition == "true" {
			compute.DefaultLogger.Info(" * Image already exists", "image", imageName, "path", imageName)
			return &Image{
				ImageName: imageName,
			}, nil
		}
	}
	compute.DefaultLogger.Info(" * Image does not exist", "image", imageName, "path", imageName)


	// otherwise, download a fresh copy
	if _, err := process.Execute(compute.Environment.PodmanBin, "pull", imageName); err != nil {
		return nil, errors.Wrapf(err, "downloading has failed")
	}


	img := &Image{
		ImageName: imageName,	
	}
	compute.DefaultLogger.Info(" * Download completed", "image", imageName, "path", img.ImageName)

	return img, nil
}

func ParseImageName(rawImageName string) string {
	// filter host
	var imageName string

	hostImage := strings.Split(rawImageName, "/")
	switch {
	case len(hostImage) == 1:
		imageName = hostImage[0]
	case len(hostImage) > 1:
		imageName = hostImage[len(hostImage)-1]
	default:
		panic("invalid name: " + rawImageName)
	}

	// filter version
	imageNameVersion := strings.Split(imageName, ":")
	switch {
	case len(imageNameVersion) == 1:
		name := imageNameVersion[0]
		version := "latest"

		return "/" + name + "_" + version + ".sif"
	case len(imageNameVersion) == 2:
		name := imageNameVersion[0]
		version := imageNameVersion[1]

		return "/" + name + "_" + version + ".sif"

	default:
		// keep the tag (version), but ignore the digest (sha256)
		// registry.k8s.io/ingress-nginx/kube-webhook-certgen:v20230407@sha256:543c40fd093964bc9ab509d3e791f9989963021f1e9e4c9c7b6700b02bfb227b
		imageNameVersionDigest := strings.Split(imageName, "@")
		digest := imageNameVersionDigest[1]
		_ = digest

		imageNameVersion = strings.Split(imageNameVersionDigest[0], ":")
		name := imageNameVersion[0]
		version := imageNameVersion[1]

		return "/" + name + "_" + version + ".sif"
	}
}
