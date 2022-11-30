import { Test } from '@nestjs/testing';
import { ApiBlogPostService } from './api-blog-post.service';

describe('ApiBlogPostService', () => {
    let service: ApiBlogPostService;

    beforeEach(async () => {
        const module = await Test.createTestingModule({
            providers: [ApiBlogPostService],
        }).compile();

        service = module.get(ApiBlogPostService);
    });

    it('should be defined', () => {
        expect(service).toBeTruthy();
    });
});
